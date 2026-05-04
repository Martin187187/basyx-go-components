#!/usr/bin/env python3
import argparse
import base64
import copy
import json
import random
import time
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Callable

import requests


def now_utc() -> datetime:
    return datetime.now(timezone.utc)


def iso_z(dt: datetime) -> str:
    return dt.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def encode_url_base64(value: str) -> str:
    return base64.urlsafe_b64encode(value.encode("utf-8")).decode("ascii").rstrip("=")


def normalize_api_prefix(prefix: str) -> str:
    value = (prefix or "").strip()
    if not value or value == "/":
        return ""
    if not value.startswith("/"):
        value = f"/{value}"
    return value.rstrip("/")


def deep_copy_json(value: Any) -> Any:
    return copy.deepcopy(value)


def parse_result_length(raw_body: bytes, content_type: str) -> int | None:
    if not raw_body:
        return 0
    if "application/json" not in (content_type or ""):
        return None
    try:
        payload = json.loads(raw_body.decode("utf-8"))
    except Exception:
        return None

    if isinstance(payload, list):
        return len(payload)
    if isinstance(payload, dict):
        result = payload.get("result")
        if isinstance(result, list):
            return len(result)
        if "paging_metadata" in payload and "result" not in payload:
            return 0
        return 1
    return None


def parse_response_count(raw_body: bytes) -> int:
    if not raw_body:
        return 1
    try:
        payload = json.loads(raw_body.decode("utf-8"))
    except Exception:
        return 1
    if isinstance(payload, list):
        return len(payload)
    return 1


def load_templates(path: Path) -> list[dict[str, Any]]:
    with path.open("r", encoding="utf-8") as file_handle:
        loaded = json.load(file_handle)
    if isinstance(loaded, list):
        return [entry for entry in loaded if isinstance(entry, dict)]
    if isinstance(loaded, dict):
        return [loaded]
    raise ValueError(f"Unsupported JSON root in template file: {path}")


def parse_weight_spec(
    spec: str | None,
    defaults: dict[str, float],
) -> dict[str, float]:
    resolved = dict(defaults)
    if spec:
        for raw_pair in spec.split(","):
            pair = raw_pair.strip()
            if not pair:
                continue
            name, _, value = pair.partition("=")
            key = name.strip()
            if key not in defaults:
                continue
            try:
                parsed = float(value.strip())
            except ValueError:
                continue
            if parsed >= 0:
                resolved[key] = parsed

    if sum(resolved.values()) <= 0:
        return dict(defaults)
    return resolved


def default_bpn_values(count: int) -> list[str]:
    return [f"BPN_COMPANY_{index:03d}" for index in range(1, count + 1)]


def default_name_value_catalog(count: int) -> list[dict[str, str]]:
    base_names = [
        "partInstanceId",
        "customerPartId",
        "manufacturerId",
        "manufacturerPartId",
        "serialNumber",
        "batchId",
        "assetLifecyclePhase",
        "plantId",
        "lineId",
        "machineId",
        "toolId",
        "orderId",
        "shipmentId",
        "supplierPartId",
        "contractId",
        "traceabilityCode",
        "materialId",
        "lotId",
        "processId",
        "stationId",
    ]

    catalog: list[dict[str, str]] = []
    for index in range(count):
        if index < len(base_names):
            name = base_names[index]
        else:
            name = f"customId{index + 1}"
        value = f"VALUE_{index + 1:03d}"
        catalog.append({"name": name, "value": value})
    return catalog


@dataclass
class SubmodelRecord:
    submodel_id: str
    encoded_id: str


@dataclass
class ShellRecord:
    shell_id: str
    encoded_id: str
    created_at: datetime
    specific_asset_ids: list[dict[str, Any]]
    asset_links: list[dict[str, str]]
    submodels: dict[str, SubmodelRecord] = field(default_factory=dict)
    global_asset_id: str | None = None
    bpn_values: list[str] = field(default_factory=list)


@dataclass
class Operation:
    name: str
    weight: float
    variants: list[str]
    handler: Callable[[str | None], dict[str, Any]]


class Stats:
    def __init__(self) -> None:
        self.count = 0
        self.success_count = 0
        self.error_count = 0
        self.total_duration_ms = 0.0
        self.min_duration_ms: float | None = None
        self.max_duration_ms: float | None = None
        self.total_response_bytes = 0
        self.total_result_length = 0
        self.result_length_samples = 0
        self.status_codes: dict[str, int] = {}

    def add(self, entry: dict[str, Any]) -> None:
        self.count += 1
        duration = float(entry.get("duration_ms", 0.0))
        self.total_duration_ms += duration
        self.min_duration_ms = duration if self.min_duration_ms is None else min(self.min_duration_ms, duration)
        self.max_duration_ms = duration if self.max_duration_ms is None else max(self.max_duration_ms, duration)
        self.total_response_bytes += int(entry.get("response_bytes", 0))

        status_code = entry.get("status_code")
        if isinstance(status_code, int):
            if 200 <= status_code <= 299:
                self.success_count += 1
            else:
                self.error_count += 1
            key = str(status_code)
            self.status_codes[key] = self.status_codes.get(key, 0) + 1
        else:
            self.error_count += 1
            self.status_codes["request_error"] = self.status_codes.get("request_error", 0) + 1

        result_length = entry.get("result_length")
        if isinstance(result_length, int):
            self.total_result_length += result_length
            self.result_length_samples += 1

    def to_dict(self) -> dict[str, Any]:
        average_duration = self.total_duration_ms / self.count if self.count > 0 else 0.0
        average_response_bytes = self.total_response_bytes / self.count if self.count > 0 else 0.0
        average_result_length = (
            self.total_result_length / self.result_length_samples
            if self.result_length_samples > 0
            else None
        )
        return {
            "count": self.count,
            "success_count": self.success_count,
            "error_count": self.error_count,
            "success_ratio": (self.success_count / self.count) if self.count > 0 else 0.0,
            "avg_duration_ms": average_duration,
            "min_duration_ms": self.min_duration_ms,
            "max_duration_ms": self.max_duration_ms,
            "avg_response_bytes": average_response_bytes,
            "avg_result_length": average_result_length,
            "status_codes": self.status_codes,
        }


class DigitalTwinRegistryBenchmark:
    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.base_url = args.base_url.rstrip("/")
        self.api_prefix = normalize_api_prefix(args.api_prefix)
        if self.api_prefix and self.base_url.endswith(self.api_prefix):
            self.api_prefix = ""
        self.rng = random.Random(args.seed)
        self.templates = load_templates(Path(args.template_json))
        if not self.templates:
            raise ValueError("No JSON object templates found in template file.")

        self.bpn_values = default_bpn_values(args.unique_bpns)
        self.name_value_catalog = default_name_value_catalog(args.unique_name_values)
        self.bpn_rotation = 0
        self.session = requests.Session()
        self.timeout = args.timeout_seconds
        self.start_time = now_utc()
        self.default_limit = args.default_limit
        self.active_auth_identity = "anonymous"
        self.admin_access_token = ""
        self.admin_access_token_expires_at = datetime.fromtimestamp(0, tz=timezone.utc)
        self.read_identity_weights = parse_weight_spec(
            args.read_identity_weights,
            {"anonymous": 30.0, "edc_header": 55.0, "admin_token": 15.0},
        )
        self.write_operations_admin_only = {
            "post_shell_descriptors",
            "put_shell_by_id",
            "delete_shell_by_id",
            "post_submodel",
            "put_submodel_by_id",
            "delete_submodel_by_id",
            "post_lookup_shells_by_id",
            "delete_lookup_shells_by_id",
        }

        self.shell_records: dict[str, ShellRecord] = {}
        self.shell_id_list: list[str] = []
        self.all_asset_link_pool: list[dict[str, str]] = []
        self.total_requests_sent = 0

        self.operations = self._build_operations(args.weights)
        self.global_stats = Stats()
        self.operation_stats: dict[str, Stats] = {operation.name: Stats() for operation in self.operations}
        self.variant_stats: dict[str, Stats] = {}

    def ensure_admin_identity_ready(self) -> None:
        _ = self._fetch_admin_access_token()

    def _fetch_admin_access_token(self, force_refresh: bool = False) -> str:
        if self.args.bearer_token:
            return self.args.bearer_token

        now = now_utc()
        if not force_refresh and self.admin_access_token:
            if now < self.admin_access_token_expires_at - timedelta(seconds=30):
                return self.admin_access_token

        form = {
            "grant_type": "password",
            "client_id": self.args.admin_client_id,
            "username": self.args.admin_username,
            "password": self.args.admin_password,
        }
        response = self.session.post(
            self.args.admin_token_url,
            data=form,
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            timeout=self.timeout,
        )
        if response.status_code != 200:
            preview = response.text
            if len(preview) > 500:
                preview = preview[:500]
            raise RuntimeError(
                f"Failed to fetch admin token (status={response.status_code}) from {self.args.admin_token_url}: {preview}"
            )

        payload = response.json()
        token = payload.get("access_token")
        if not isinstance(token, str) or token == "":
            raise RuntimeError("Token response did not contain access_token")

        expires_in = payload.get("expires_in", 300)
        try:
            expires_seconds = int(expires_in)
        except (TypeError, ValueError):
            expires_seconds = 300

        self.admin_access_token = token
        self.admin_access_token_expires_at = now + timedelta(seconds=max(expires_seconds, 60))
        return token

    def _pick_identity_for_operation(self, op_name: str) -> str:
        if op_name in self.write_operations_admin_only:
            return "admin_token"

        identities = list(self.read_identity_weights.keys())
        weights = list(self.read_identity_weights.values())
        return self.rng.choices(identities, weights=weights, k=1)[0]

    def _build_operations(self, weights_override: str | None) -> list[Operation]:
        default_weights = {
            "get_description": 1.0,
            "get_shell_descriptors": 1.0,
            "post_shell_descriptors": 1.0,
            "get_shell_by_id": 1.0,
            "put_shell_by_id": 1.0,
            "delete_shell_by_id": 1.0,
            "get_submodels": 1.0,
            "post_submodel": 1.0,
            "get_submodel_by_id": 1.0,
            "put_submodel_by_id": 1.0,
            "delete_submodel_by_id": 1.0,
            "query_shell_descriptors": 1.0,
            "get_lookup_shells": 1.0,
            "post_lookup_shells_by_asset_link": 1.0,
            "get_lookup_shells_by_id": 1.0,
            "post_lookup_shells_by_id": 1.0,
            "delete_lookup_shells_by_id": 1.0,
        }
        if weights_override:
            for raw_pair in weights_override.split(","):
                pair = raw_pair.strip()
                if not pair:
                    continue
                name, _, value = pair.partition("=")
                if name in default_weights and value:
                    default_weights[name] = float(value)

        operations = [
            Operation("get_description", default_weights["get_description"], ["plain"], self.op_get_description),
            Operation(
                "get_shell_descriptors",
                default_weights["get_shell_descriptors"],
                ["plain", "limit", "limit_cursor", "asset_kind", "asset_type", "created_after", "combined"],
                self.op_get_shell_descriptors,
            ),
            Operation(
                "post_shell_descriptors",
                default_weights["post_shell_descriptors"],
                ["generated"],
                self.op_post_shell_descriptors,
            ),
            Operation(
                "get_shell_by_id",
                default_weights["get_shell_by_id"],
                ["existing", "unknown"],
                self.op_get_shell_by_id,
            ),
            Operation(
                "put_shell_by_id",
                default_weights["put_shell_by_id"],
                ["update_existing", "upsert_new", "path_wins"],
                self.op_put_shell_by_id,
            ),
            Operation(
                "delete_shell_by_id",
                default_weights["delete_shell_by_id"],
                ["existing", "unknown"],
                self.op_delete_shell_by_id,
            ),
            Operation(
                "get_submodels",
                default_weights["get_submodels"],
                ["plain", "limit", "limit_cursor"],
                self.op_get_submodels,
            ),
            Operation(
                "post_submodel",
                default_weights["post_submodel"],
                ["append_submodel"],
                self.op_post_submodel,
            ),
            Operation(
                "get_submodel_by_id",
                default_weights["get_submodel_by_id"],
                ["existing", "unknown"],
                self.op_get_submodel_by_id,
            ),
            Operation(
                "put_submodel_by_id",
                default_weights["put_submodel_by_id"],
                ["update_existing", "upsert_new", "path_wins"],
                self.op_put_submodel_by_id,
            ),
            Operation(
                "delete_submodel_by_id",
                default_weights["delete_submodel_by_id"],
                ["existing", "unknown"],
                self.op_delete_submodel_by_id,
            ),
            Operation(
                "query_shell_descriptors",
                default_weights["query_shell_descriptors"],
                ["by_id", "by_name_value", "by_bpn"],
                self.op_query_shell_descriptors,
            ),
            Operation(
                "get_lookup_shells",
                default_weights["get_lookup_shells"],
                ["single"],
                self.op_get_lookup_shells,
            ),
            Operation(
                "post_lookup_shells_by_asset_link",
                default_weights["post_lookup_shells_by_asset_link"],
                ["single"],
                self.op_post_lookup_shells_by_asset_link,
            ),
            Operation(
                "get_lookup_shells_by_id",
                default_weights["get_lookup_shells_by_id"],
                ["existing", "unknown"],
                self.op_get_lookup_shells_by_id,
            ),
            Operation(
                "post_lookup_shells_by_id",
                default_weights["post_lookup_shells_by_id"],
                ["append_links", "plain_only"],
                self.op_post_lookup_shells_by_id,
            ),
            Operation(
                "delete_lookup_shells_by_id",
                default_weights["delete_lookup_shells_by_id"],
                ["existing", "unknown"],
                self.op_delete_lookup_shells_by_id,
            ),
        ]
        return operations

    def _pick_operation(self) -> Operation:
        choices = [op.name for op in self.operations]
        weights = [op.weight for op in self.operations]
        selected_name = self.rng.choices(choices, weights=weights, k=1)[0]
        for operation in self.operations:
            if operation.name == selected_name:
                return operation
        return self.operations[0]

    def _pick_variant(self, operation: Operation) -> str:
        if len(operation.variants) == 1:
            return operation.variants[0]
        return self.rng.choice(operation.variants)

    def _ensure_shell(self, min_submodels: int = 0) -> ShellRecord:
        for _ in range(8):
            candidate = self._pick_shell()
            if candidate is not None and len(candidate.submodels) >= min_submodels:
                return candidate

        payload, record = self._build_shell_payload()
        created = self._post_shell(payload, "on_demand_seed")
        if created is not None:
            return created
        return record

    def _pick_shell(self) -> ShellRecord | None:
        if not self.shell_id_list:
            return None
        shell_id = self.rng.choice(self.shell_id_list)
        return self.shell_records.get(shell_id)

    def _pick_submodel(self, shell: ShellRecord) -> SubmodelRecord | None:
        if not shell.submodels:
            return None
        key = self.rng.choice(list(shell.submodels.keys()))
        return shell.submodels[key]

    def _next_bpn(self) -> str:
        value = self.bpn_values[self.bpn_rotation % len(self.bpn_values)]
        self.bpn_rotation += 1
        return value

    def _random_bpn(self) -> str:
        if self.rng.random() < 0.5:
            return self._next_bpn()
        return self.rng.choice(self.bpn_values)

    def _random_name(self, candidate_names: list[str], used: set[str], fallback_prefix: str) -> str:
        for _ in range(20):
            name = self.rng.choice(candidate_names)
            if name not in used:
                used.add(name)
                return name
        generated = f"{fallback_prefix}{len(used) + 1}"
        used.add(generated)
        return generated

    def _random_value(self, name: str) -> str:
        lowered = name.lower()
        if "part" in lowered or "instance" in lowered:
            return f"{self.rng.randint(10_000_000, 99_999_999)}"
        if "manufacturer" in lowered or "customer" in lowered:
            return f"{self.rng.randint(100_000, 999_999)}"
        return f"{uuid.uuid4()}"

    def _sanitize_template(self, template: dict[str, Any]) -> dict[str, Any]:
        payload = deep_copy_json(template)
        payload.pop("createdAt", None)
        payload["id"] = f"urn:uuid:{uuid.uuid4()}"

        descriptors = payload.get("submodelDescriptors")
        if not isinstance(descriptors, list) or len(descriptors) == 0:
            payload["submodelDescriptors"] = [self._default_submodel_descriptor()]
        else:
            sanitized_submodels = []
            for raw_submodel in descriptors:
                if not isinstance(raw_submodel, dict):
                    continue
                submodel = deep_copy_json(raw_submodel)
                submodel.pop("createdAt", None)
                submodel["id"] = f"urn:uuid:{uuid.uuid4()}"
                if not submodel.get("idShort"):
                    submodel["idShort"] = f"sm-{uuid.uuid4().hex[:8]}"
                sanitized_submodels.append(submodel)
            if not sanitized_submodels:
                sanitized_submodels = [self._default_submodel_descriptor()]
            payload["submodelDescriptors"] = sanitized_submodels

        if not payload.get("idShort"):
            payload["idShort"] = f"shell-{uuid.uuid4().hex[:8]}"

        return payload

    def _default_submodel_descriptor(self) -> dict[str, Any]:
        return {
            "idShort": f"sm-{uuid.uuid4().hex[:8]}",
            "id": f"urn:uuid:{uuid.uuid4()}",
            "semanticId": {
                "type": "ExternalReference",
                "keys": [{"type": "GlobalReference", "value": "urn:example:semantic:default"}],
            },
            "endpoints": [
                {
                    "interface": "SUBMODEL-3.0",
                    "protocolInformation": {
                        "href": f"https://example.org/submodel/{uuid.uuid4()}",
                        "endpointProtocol": "HTTP",
                        "endpointProtocolVersion": ["1.1"],
                        "subprotocol": "DSP",
                        "subprotocolBody": "id=benchmark;dspEndpoint=https://example.org/dsp",
                        "subprotocolBodyEncoding": "plain",
                        "securityAttributes": [
                            {"type": "NONE", "key": "NONE", "value": "NONE"},
                        ],
                    },
                },
            ],
        }

    def _build_specific_asset_ids(
        self,
        force_bpn: bool,
    ) -> tuple[list[dict[str, Any]], list[dict[str, str]], list[str]]:
        used_pairs: set[tuple[str, str]] = set()
        specific_asset_ids: list[dict[str, Any]] = []
        asset_links: list[dict[str, str]] = []
        bpn_used: list[str] = []

        def pick_pair() -> tuple[str, str]:
            available = [
                pair
                for pair in self.name_value_catalog
                if (pair["name"], pair["value"]) not in used_pairs
            ]
            source = available if available else self.name_value_catalog
            chosen = self.rng.choice(source)
            pair = (chosen["name"], chosen["value"])
            used_pairs.add(pair)
            return pair

        plain_name, plain_value = pick_pair()
        specific_asset_ids.append({"name": plain_name, "value": plain_value})
        asset_links.append({"name": plain_name, "value": plain_value})

        external_count = self.rng.randint(0, 3)
        for _ in range(external_count):
            entry_name, entry_value = pick_pair()
            bpn_count = 1 if self.rng.random() < 0.8 else 2
            keys = []
            for _ in range(bpn_count):
                bpn = self._random_bpn()
                keys.append({"type": "GlobalReference", "value": bpn})
                bpn_used.append(bpn)
            specific_asset_ids.append(
                {
                    "name": entry_name,
                    "value": entry_value,
                    "externalSubjectId": {
                        "type": "ExternalReference",
                        "keys": keys,
                    },
                }
            )
            asset_links.append({"name": entry_name, "value": entry_value})

        public_count = self.rng.randint(0, 1)
        for _ in range(public_count):
            entry_name, entry_value = pick_pair()
            specific_asset_ids.append(
                {
                    "name": entry_name,
                    "value": entry_value,
                    "externalSubjectId": {
                        "type": "ExternalReference",
                        "keys": [{"type": "GlobalReference", "value": "PUBLIC_READABLE"}],
                    },
                }
            )
            asset_links.append({"name": entry_name, "value": entry_value})

        if force_bpn and not bpn_used:
            forced_name, forced_value = pick_pair()
            forced_bpn = self._next_bpn()
            specific_asset_ids.append(
                {
                    "name": forced_name,
                    "value": forced_value,
                    "externalSubjectId": {
                        "type": "ExternalReference",
                        "keys": [{"type": "GlobalReference", "value": forced_bpn}],
                    },
                }
            )
            asset_links.append({"name": forced_name, "value": forced_value})
            bpn_used.append(forced_bpn)

        return specific_asset_ids, asset_links, bpn_used

    def _build_shell_payload(
        self,
        force_global: bool | None = None,
        force_bpn: bool = False,
    ) -> tuple[dict[str, Any], ShellRecord]:
        template = self.rng.choice(self.templates)
        payload = self._sanitize_template(template)

        specific_asset_ids, asset_links, bpn_used = self._build_specific_asset_ids(
            force_bpn=force_bpn,
        )
        payload["specificAssetIds"] = specific_asset_ids

        add_global = force_global if force_global is not None else (self.rng.random() < 0.5)
        global_asset_id = None
        if add_global:
            global_asset_id = f"urn:uuid:{uuid.uuid4()}"
            payload["globalAssetId"] = global_asset_id
            asset_links.append({"name": "globalAssetId", "value": global_asset_id})
        else:
            payload.pop("globalAssetId", None)

        created_at = now_utc()
        shell_id = payload["id"]
        encoded_id = encode_url_base64(shell_id)

        submodels: dict[str, SubmodelRecord] = {}
        for submodel in payload.get("submodelDescriptors", []):
            if not isinstance(submodel, dict):
                continue
            sub_id = str(submodel.get("id", f"urn:uuid:{uuid.uuid4()}"))
            submodel["id"] = sub_id
            submodels[sub_id] = SubmodelRecord(submodel_id=sub_id, encoded_id=encode_url_base64(sub_id))

        record = ShellRecord(
            shell_id=shell_id,
            encoded_id=encoded_id,
            created_at=created_at,
            specific_asset_ids=deep_copy_json(specific_asset_ids),
            asset_links=deep_copy_json(asset_links),
            submodels=submodels,
            global_asset_id=global_asset_id,
            bpn_values=sorted(set(bpn_used)),
        )
        return payload, record

    def _register_shell_record(self, record: ShellRecord) -> None:
        existing = self.shell_records.get(record.shell_id)
        self.shell_records[record.shell_id] = record
        if existing is None:
            self.shell_id_list.append(record.shell_id)
        for link in record.asset_links:
            self.all_asset_link_pool.append({"name": link["name"], "value": link["value"]})

    def _remove_shell_record(self, shell_id: str) -> None:
        if shell_id in self.shell_records:
            del self.shell_records[shell_id]
        self.shell_id_list = [candidate for candidate in self.shell_id_list if candidate != shell_id]

    def _header_for_request(
        self,
        op_name: str,
        preferred_bpns: list[str] | None = None,
        force_admin: bool = False,
    ) -> dict[str, str]:
        headers: dict[str, str] = {}
        identity = "admin_token" if force_admin else self._pick_identity_for_operation(op_name)

        if identity == "admin_token":
            token = self._fetch_admin_access_token()
            headers["Authorization"] = f"Bearer {token}"

        mode = self.args.edc_header_mode
        include_bpn = False
        if identity == "edc_header":
            include_bpn = mode != "never"
        elif identity == "admin_token":
            include_bpn = mode == "always" or (mode == "random" and self.rng.random() < 0.5)
        else:
            include_bpn = mode == "always"

        if include_bpn:
            if preferred_bpns:
                headers["Edc-Bpn"] = self.rng.choice(preferred_bpns)
            else:
                headers["Edc-Bpn"] = self._random_bpn()

        self.active_auth_identity = identity
        return headers

    def _created_after_param(self, matching_data: bool) -> str:
        if matching_data:
            point = self.start_time - timedelta(minutes=2)
            return iso_z(point)
        point = now_utc() + timedelta(minutes=5)
        return iso_z(point)

    def _random_asset_link(self) -> dict[str, str]:
        if self.all_asset_link_pool:
            return deep_copy_json(self.rng.choice(self.all_asset_link_pool))
        shell = self._ensure_shell()
        if shell.asset_links:
            return deep_copy_json(self.rng.choice(shell.asset_links))
        return {"name": "fallbackId", "value": uuid.uuid4().hex}

    def _dedupe_links(self, links: list[dict[str, str]]) -> list[dict[str, str]]:
        deduped: list[dict[str, str]] = []
        seen: set[tuple[str, str]] = set()
        for link in links:
            name = link.get("name")
            value = link.get("value")
            if not isinstance(name, str) or not isinstance(value, str):
                continue
            key = (name, value)
            if key in seen:
                continue
            seen.add(key)
            deduped.append({"name": name, "value": value})
        return deduped

    def _lookup_preferred_links(
        self,
        shell: ShellRecord,
        headers: dict[str, str],
    ) -> tuple[list[dict[str, str]], list[dict[str, str]]]:
        all_links = self._dedupe_links(deep_copy_json(shell.asset_links))
        public_links: list[dict[str, str]] = []
        bpn_links: list[dict[str, str]] = []
        edc_bpn = str(headers.get("Edc-Bpn", "")).strip()

        for entry in shell.specific_asset_ids:
            if not isinstance(entry, dict):
                continue
            name = entry.get("name")
            value = entry.get("value")
            if not isinstance(name, str) or not isinstance(value, str):
                continue
            link = {"name": name, "value": value}
            external = entry.get("externalSubjectId")
            key_values: set[str] = set()
            if isinstance(external, dict):
                keys = external.get("keys")
                if isinstance(keys, list):
                    for key in keys:
                        if isinstance(key, dict):
                            raw_value = key.get("value")
                            if isinstance(raw_value, str):
                                key_values.add(raw_value)
            if "PUBLIC_READABLE" in key_values:
                public_links.append(link)
            if edc_bpn and edc_bpn in key_values:
                bpn_links.append(link)

        public_links = self._dedupe_links(public_links)
        bpn_links = self._dedupe_links(bpn_links)
        preferred_links = self._dedupe_links(public_links + bpn_links) if edc_bpn else public_links
        return all_links, preferred_links

    def _choose_lookup_body_links(
        self,
        shell: ShellRecord,
        variant: str,
        headers: dict[str, str],
    ) -> list[dict[str, str]]:
        if variant == "empty":
            return []

        all_links, preferred_links = self._lookup_preferred_links(shell, headers)
        if not all_links:
            return [self._random_asset_link()]

        use_result_focused = bool(preferred_links) and self.rng.random() < self.args.lookup_result_bias
        source_links = preferred_links if use_result_focused else all_links
        if not source_links:
            source_links = all_links

        if variant == "single":
            return [deep_copy_json(self.rng.choice(source_links))]
        if variant == "multi":
            if use_result_focused:
                max_count = min(4, len(source_links))
                if max_count == 0:
                    return [deep_copy_json(self.rng.choice(all_links))]
                min_count = 1 if max_count == 1 else 2
                target_count = self.rng.randint(min_count, max_count)
                return deep_copy_json(source_links[:target_count])
            return deep_copy_json(all_links)
        if variant == "limit_cursor":
            return deep_copy_json(source_links[:2] if len(source_links) >= 2 else source_links)
        if variant == "created_after":
            return deep_copy_json(source_links[:3] if len(source_links) >= 3 else source_links)
        return deep_copy_json(source_links)

    def _asset_ids_query_values(self, links: list[dict[str, str]]) -> list[tuple[str, str]]:
        encoded_values = []
        for link in links:
            compact = json.dumps(link, separators=(",", ":"))
            encoded_values.append(("assetIds", encode_url_base64(compact)))
        return encoded_values

    def _extract_submodel_payload(self) -> dict[str, Any]:
        return self._default_submodel_descriptor()

    def _request(
        self,
        *,
        op_name: str,
        variant: str,
        method: str,
        path: str,
        params: dict[str, Any] | list[tuple[str, Any]] | None = None,
        body: Any = None,
        headers: dict[str, str] | None = None,
        shell_id: str | None = None,
        submodel_id: str | None = None,
    ) -> dict[str, Any]:
        url = f"{self.base_url}{self.api_prefix}{path}"
        merged_headers = dict(headers or {})
        auth_identity = self.active_auth_identity

        started = time.perf_counter()
        status_code: int | None = None
        response_bytes = 0
        result_length: int | None = None
        count = 1
        response_body_preview: str | None = None
        request_error: str | None = None
        final_url = url

        try:
            response = self.session.request(
                method=method,
                url=url,
                params=params,
                json=body,
                headers=merged_headers if merged_headers else None,
                timeout=self.timeout,
            )
            if response.status_code == 401 and auth_identity == "admin_token":
                refreshed_token = self._fetch_admin_access_token(force_refresh=True)
                merged_headers["Authorization"] = f"Bearer {refreshed_token}"
                response = self.session.request(
                    method=method,
                    url=url,
                    params=params,
                    json=body,
                    headers=merged_headers,
                    timeout=self.timeout,
                )
            final_url = response.url
            status_code = response.status_code
            response_bytes = len(response.content or b"")
            result_length = parse_result_length(response.content or b"", response.headers.get("Content-Type", ""))
            count = parse_response_count(response.content or b"")
            if self.args.store_response_body:
                preview = response.text
                if len(preview) > self.args.response_preview_chars:
                    preview = preview[: self.args.response_preview_chars]
                response_body_preview = preview
        except Exception as exc:
            request_error = str(exc)

        finished = time.perf_counter()
        duration_ms = (finished - started) * 1000.0
        self.total_requests_sent += 1

        entry: dict[str, Any] = {
            "iteration": self.total_requests_sent,
            "timestamp": iso_z(now_utc()),
            "auth_identity": auth_identity,
            "operation": op_name,
            "variant": variant,
            "method": method,
            "url": final_url,
            "status_code": status_code,
            "duration_ms": round(duration_ms, 3),
            "response_bytes": response_bytes,
            "count": count,
            "result_length": result_length,
            "shell_id": shell_id,
            "submodel_id": submodel_id,
            "edc_bpn": merged_headers.get("Edc-Bpn"),
            "request_body_size": len(json.dumps(body)) if body is not None else 0,
            "request_error": request_error,
        }
        if self.args.store_request_body and body is not None:
            entry["request_body"] = body
        if response_body_preview is not None:
            entry["response_body"] = response_body_preview

        self._add_stats(entry, op_name, variant)
        return entry

    def _add_stats(self, entry: dict[str, Any], op_name: str, variant: str) -> None:
        self.global_stats.add(entry)
        self.operation_stats[op_name].add(entry)
        key = f"{op_name}:{variant}"
        if key not in self.variant_stats:
            self.variant_stats[key] = Stats()
        self.variant_stats[key].add(entry)

    def _post_shell(self, payload: dict[str, Any], variant: str) -> ShellRecord | None:
        shell_id = payload["id"]
        provisional_record = self._record_from_payload(payload)
        entry = self._request(
            op_name="post_shell_descriptors",
            variant=variant,
            method="POST",
            path="/shell-descriptors",
            body=payload,
            headers=self._header_for_request("post_shell_descriptors", provisional_record.bpn_values, force_admin=True),
            shell_id=shell_id,
        )
        if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
            self._register_shell_record(provisional_record)
            return provisional_record
        return None

    def _record_from_payload(self, payload: dict[str, Any]) -> ShellRecord:
        shell_id = str(payload["id"])
        encoded = encode_url_base64(shell_id)
        specific_asset_ids = payload.get("specificAssetIds", [])
        asset_links = []
        bpn_values = []
        if isinstance(specific_asset_ids, list):
            for entry in specific_asset_ids:
                if not isinstance(entry, dict):
                    continue
                name = entry.get("name")
                value = entry.get("value")
                if isinstance(name, str) and isinstance(value, str):
                    asset_links.append({"name": name, "value": value})
                external = entry.get("externalSubjectId")
                if isinstance(external, dict):
                    keys = external.get("keys")
                    if isinstance(keys, list):
                        for key in keys:
                            if isinstance(key, dict):
                                bpn = key.get("value")
                                if isinstance(bpn, str) and bpn != "PUBLIC_READABLE":
                                    bpn_values.append(bpn)

        global_asset_id = payload.get("globalAssetId")
        if isinstance(global_asset_id, str) and global_asset_id:
            asset_links.append({"name": "globalAssetId", "value": global_asset_id})

        submodels: dict[str, SubmodelRecord] = {}
        for submodel in payload.get("submodelDescriptors", []):
            if isinstance(submodel, dict):
                sub_id = submodel.get("id")
                if isinstance(sub_id, str):
                    submodels[sub_id] = SubmodelRecord(sub_id, encode_url_base64(sub_id))

        return ShellRecord(
            shell_id=shell_id,
            encoded_id=encoded,
            created_at=now_utc(),
            specific_asset_ids=deep_copy_json(specific_asset_ids),
            asset_links=asset_links,
            submodels=submodels,
            global_asset_id=global_asset_id if isinstance(global_asset_id, str) else None,
            bpn_values=sorted(set(bpn_values)),
        )

    def seed_initial_shells(self, count: int, log_handle: Any | None = None) -> None:
        for index in range(count):
            force_bpn = index < len(self.bpn_values)
            payload, record = self._build_shell_payload(force_bpn=force_bpn)
            entry = self._request(
                op_name="post_shell_descriptors",
                variant="seed",
                method="POST",
                path="/shell-descriptors",
                body=payload,
                headers=self._header_for_request("post_shell_descriptors", record.bpn_values, force_admin=True),
                shell_id=record.shell_id,
            )
            if log_handle is not None:
                log_handle.write(json.dumps(entry, separators=(",", ":")) + "\n")
            if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
                self._register_shell_record(record)

    def run_coverage_once(self, log_handle: Any | None = None) -> None:
        for operation in self.operations:
            for variant in operation.variants:
                entry = operation.handler(variant)
                if log_handle is not None:
                    log_handle.write(json.dumps(entry, separators=(",", ":")) + "\n")

    def run_weighted_requests(self, request_count: int, log_handle: Any) -> None:
        for index in range(request_count):
            operation = self._pick_operation()
            variant = self._pick_variant(operation)
            entry = operation.handler(variant)
            log_handle.write(json.dumps(entry, separators=(",", ":")) + "\n")

            if self.args.progress_every > 0 and (index + 1) % self.args.progress_every == 0:
                elapsed = (now_utc() - self.start_time).total_seconds()
                throughput = (self.total_requests_sent / elapsed) if elapsed > 0 else 0.0
                print(
                    f"progress requests={index + 1}/{request_count} "
                    f"tracked_shells={len(self.shell_records)} throughput_rps={throughput:.2f}"
                )

    def write_summary(self, path: Path) -> None:
        summary = {
            "base_url": self.base_url,
            "api_prefix": self.api_prefix,
            "read_identity_weights": self.read_identity_weights,
            "admin_token_url": self.args.admin_token_url,
            "admin_client_id": self.args.admin_client_id,
            "admin_username": self.args.admin_username,
            "default_limit": self.default_limit,
            "unique_name_values": self.args.unique_name_values,
            "unique_bpns": self.args.unique_bpns,
            "started_at": iso_z(self.start_time),
            "finished_at": iso_z(now_utc()),
            "total_requests": self.total_requests_sent,
            "tracked_shells": len(self.shell_records),
            "bpn_values": self.bpn_values,
            "global": self.global_stats.to_dict(),
            "operations": {name: stats.to_dict() for name, stats in self.operation_stats.items()},
            "variants": {name: stats.to_dict() for name, stats in self.variant_stats.items()},
        }
        with path.open("w", encoding="utf-8") as file_handle:
            json.dump(summary, file_handle, indent=2)

    def op_get_description(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or "plain"
        return self._request(
            op_name="get_description",
            variant=variant,
            method="GET",
            path="/description",
            headers=self._header_for_request("get_description"),
        )

    def op_get_shell_descriptors(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or "plain"
        shell = self._pick_shell()
        params: dict[str, Any] = {"limit": self.default_limit}
        if variant == "limit":
            pass
        elif variant == "limit_cursor" and shell is not None:
            params["cursor"] = shell.encoded_id
        elif variant == "asset_kind":
            params["assetKind"] = "Instance"
        elif variant == "asset_type":
            params["assetType"] = "PartInstance"
        elif variant == "created_after":
            params["createdAfter"] = self._created_after_param(matching_data=True)
        elif variant == "combined" and shell is not None:
            params = {
                "limit": self.default_limit,
                "cursor": shell.encoded_id,
                "assetKind": "Instance",
                "assetType": "PartInstance",
                "createdAfter": self._created_after_param(matching_data=True),
            }
        return self._request(
            op_name="get_shell_descriptors",
            variant=variant,
            method="GET",
            path="/shell-descriptors",
            params=params if params else None,
            headers=self._header_for_request("get_shell_descriptors", shell.bpn_values if shell else None),
        )

    def op_post_shell_descriptors(self, forced_variant: str | None = None) -> dict[str, Any]:
        _ = forced_variant or "generated"
        payload, record = self._build_shell_payload()
        entry = self._request(
            op_name="post_shell_descriptors",
            variant="generated",
            method="POST",
            path="/shell-descriptors",
            body=payload,
            headers=self._header_for_request("post_shell_descriptors", record.bpn_values, force_admin=True),
            shell_id=record.shell_id,
        )
        if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
            self._register_shell_record(record)
        return entry

    def op_get_shell_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or ("unknown" if self.rng.random() < 0.08 else "existing")
        shell = self._pick_shell() or self._ensure_shell()
        target_id = shell.encoded_id if variant == "existing" else encode_url_base64(f"urn:uuid:{uuid.uuid4()}")
        return self._request(
            op_name="get_shell_by_id",
            variant=variant,
            method="GET",
            path=f"/shell-descriptors/{target_id}",
            headers=self._header_for_request("get_shell_by_id", shell.bpn_values),
            shell_id=shell.shell_id if variant == "existing" else None,
        )

    def op_put_shell_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or self.rng.choice(["update_existing", "upsert_new", "path_wins"])
        if variant == "upsert_new":
            payload, record = self._build_shell_payload(force_global=self.rng.random() < 0.5)
            target_shell_id = f"urn:uuid:{uuid.uuid4()}"
            payload["id"] = payload["id"]
            encoded_target = encode_url_base64(target_shell_id)
            entry = self._request(
                op_name="put_shell_by_id",
                variant=variant,
                method="PUT",
                path=f"/shell-descriptors/{encoded_target}",
                body=payload,
                headers=self._header_for_request("put_shell_by_id", record.bpn_values),
                shell_id=target_shell_id,
            )
            if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
                payload["id"] = target_shell_id
                self._register_shell_record(self._record_from_payload(payload))
            return entry

        shell = self._ensure_shell()
        payload, _ = self._build_shell_payload(force_global=self.rng.random() < 0.5)
        if variant == "update_existing":
            payload["id"] = shell.shell_id
        else:
            payload["id"] = f"urn:uuid:{uuid.uuid4()}"

        entry = self._request(
            op_name="put_shell_by_id",
            variant=variant,
            method="PUT",
            path=f"/shell-descriptors/{shell.encoded_id}",
            body=payload,
            headers=self._header_for_request("put_shell_by_id", shell.bpn_values),
            shell_id=shell.shell_id,
        )
        if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
            payload["id"] = shell.shell_id
            self._register_shell_record(self._record_from_payload(payload))
        return entry

    def op_delete_shell_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or ("unknown" if self.rng.random() < 0.2 else "existing")
        shell = self._pick_shell() or self._ensure_shell()
        encoded_id = shell.encoded_id if variant == "existing" else encode_url_base64(f"urn:uuid:{uuid.uuid4()}")
        entry = self._request(
            op_name="delete_shell_by_id",
            variant=variant,
            method="DELETE",
            path=f"/shell-descriptors/{encoded_id}",
            headers=self._header_for_request("delete_shell_by_id", shell.bpn_values),
            shell_id=shell.shell_id if variant == "existing" else None,
        )
        if variant == "existing" and isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
            self._remove_shell_record(shell.shell_id)
        return entry

    def op_get_submodels(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or self.rng.choice(["plain", "limit", "limit_cursor"])
        shell = self._ensure_shell(min_submodels=1)
        params: dict[str, Any] = {"limit": self.default_limit}
        if variant == "limit":
            pass
        elif variant == "limit_cursor":
            submodel = self._pick_submodel(shell)
            if submodel is not None:
                params["cursor"] = submodel.encoded_id
        return self._request(
            op_name="get_submodels",
            variant=variant,
            method="GET",
            path=f"/shell-descriptors/{shell.encoded_id}/submodel-descriptors",
            params=params if params else None,
            headers=self._header_for_request("get_submodels", shell.bpn_values),
            shell_id=shell.shell_id,
        )

    def op_post_submodel(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or "append_submodel"
        shell = self._ensure_shell()
        submodel_payload = self._extract_submodel_payload()
        submodel_id = submodel_payload["id"]
        entry = self._request(
            op_name="post_submodel",
            variant=variant,
            method="POST",
            path=f"/shell-descriptors/{shell.encoded_id}/submodel-descriptors",
            body=submodel_payload,
            headers=self._header_for_request("post_submodel", shell.bpn_values),
            shell_id=shell.shell_id,
            submodel_id=submodel_id,
        )
        if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
            shell.submodels[submodel_id] = SubmodelRecord(submodel_id, encode_url_base64(submodel_id))
        return entry

    def op_get_submodel_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or ("unknown" if self.rng.random() < 0.1 else "existing")
        shell = self._ensure_shell(min_submodels=1)
        submodel = self._pick_submodel(shell)
        if submodel is None:
            shell = self._ensure_shell(min_submodels=1)
            submodel = self._pick_submodel(shell)
        submodel_id = submodel.encoded_id if (submodel and variant == "existing") else encode_url_base64(f"urn:uuid:{uuid.uuid4()}")
        return self._request(
            op_name="get_submodel_by_id",
            variant=variant,
            method="GET",
            path=f"/shell-descriptors/{shell.encoded_id}/submodel-descriptors/{submodel_id}",
            headers=self._header_for_request("get_submodel_by_id", shell.bpn_values),
            shell_id=shell.shell_id,
            submodel_id=submodel.submodel_id if (submodel and variant == "existing") else None,
        )

    def op_put_submodel_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or self.rng.choice(["update_existing", "upsert_new", "path_wins"])
        shell = self._ensure_shell()
        payload = self._extract_submodel_payload()

        if variant == "upsert_new":
            target_id = f"urn:uuid:{uuid.uuid4()}"
            encoded_target = encode_url_base64(target_id)
            entry = self._request(
                op_name="put_submodel_by_id",
                variant=variant,
                method="PUT",
                path=f"/shell-descriptors/{shell.encoded_id}/submodel-descriptors/{encoded_target}",
                body=payload,
                headers=self._header_for_request("put_submodel_by_id", shell.bpn_values),
                shell_id=shell.shell_id,
                submodel_id=target_id,
            )
            if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
                shell.submodels[target_id] = SubmodelRecord(target_id, encoded_target)
            return entry

        if not shell.submodels:
            self.op_post_submodel("append_submodel")
        submodel = self._pick_submodel(shell)
        if submodel is None:
            payload["id"] = f"urn:uuid:{uuid.uuid4()}"
            target = encode_url_base64(payload["id"])
            return self._request(
                op_name="put_submodel_by_id",
                variant=variant,
                method="PUT",
                path=f"/shell-descriptors/{shell.encoded_id}/submodel-descriptors/{target}",
                body=payload,
                headers=self._header_for_request("put_submodel_by_id", shell.bpn_values),
                shell_id=shell.shell_id,
            )

        payload["id"] = submodel.submodel_id if variant == "update_existing" else f"urn:uuid:{uuid.uuid4()}"
        entry = self._request(
            op_name="put_submodel_by_id",
            variant=variant,
            method="PUT",
            path=f"/shell-descriptors/{shell.encoded_id}/submodel-descriptors/{submodel.encoded_id}",
            body=payload,
            headers=self._header_for_request("put_submodel_by_id", shell.bpn_values),
            shell_id=shell.shell_id,
            submodel_id=submodel.submodel_id,
        )
        if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
            shell.submodels[submodel.submodel_id] = SubmodelRecord(submodel.submodel_id, submodel.encoded_id)
        return entry

    def op_delete_submodel_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or ("unknown" if self.rng.random() < 0.2 else "existing")
        shell = self._ensure_shell(min_submodels=1)
        submodel = self._pick_submodel(shell)
        if submodel is None:
            return self.op_post_submodel("append_submodel")
        encoded_target = submodel.encoded_id if variant == "existing" else encode_url_base64(f"urn:uuid:{uuid.uuid4()}")
        entry = self._request(
            op_name="delete_submodel_by_id",
            variant=variant,
            method="DELETE",
            path=f"/shell-descriptors/{shell.encoded_id}/submodel-descriptors/{encoded_target}",
            headers=self._header_for_request("delete_submodel_by_id", shell.bpn_values),
            shell_id=shell.shell_id,
            submodel_id=submodel.submodel_id if variant == "existing" else None,
        )
        if variant == "existing" and isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
            shell.submodels.pop(submodel.submodel_id, None)
        return entry

    def op_query_shell_descriptors(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or self.rng.choice(["by_id", "by_name_value", "by_bpn"])
        shell = self._ensure_shell()
        if variant == "by_id":
            body = {
                "$condition": {
                    "$eq": [
                        {"$field": "$aasdesc#id"},
                        {"$strVal": shell.shell_id},
                    ]
                }
            }
        elif variant == "by_name_value":
            link = self.rng.choice(shell.asset_links) if shell.asset_links else self._random_asset_link()
            body = {
                "$condition": {
                    "$and": [
                        {"$eq": [{"$field": "$aasdesc#specificAssetIds[].name"}, {"$strVal": link["name"]}]},
                        {"$eq": [{"$field": "$aasdesc#specificAssetIds[].value"}, {"$strVal": link["value"]}]},
                    ]
                },
                "$filters": [
                    {
                        "$fragment": "$aasdesc#specificAssetIds[]",
                        "$condition": {
                            "$eq": [
                                {"$field": "$aasdesc#specificAssetIds[].name"},
                                {"$strVal": link["name"]},
                            ]
                        },
                    }
                ],
            }
        else:
            bpn = shell.bpn_values[0] if shell.bpn_values else self._random_bpn()
            body = {
                "$condition": {
                    "$eq": [
                        {"$field": "$aasdesc#specificAssetIds[].externalSubjectId.keys[].value"},
                        {"$strVal": bpn},
                    ]
                }
            }
        params = {"limit": self.default_limit}
        if self.rng.random() < 0.4:
            params["cursor"] = shell.encoded_id
        return self._request(
            op_name="query_shell_descriptors",
            variant=variant,
            method="POST",
            path="/query/shell-descriptors",
            params=params,
            body=body,
            headers=self._header_for_request("query_shell_descriptors", shell.bpn_values),
            shell_id=shell.shell_id,
        )

    def op_get_lookup_shells(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or self.rng.choice(["single", "multi", "limit_cursor", "created_after", "empty"])
        shell = self._pick_shell() or self._ensure_shell()
        headers = self._header_for_request("get_lookup_shells", shell.bpn_values)
        params_list: list[tuple[str, Any]] = [("limit", self.default_limit)]
        chosen_links = self._choose_lookup_body_links(shell, variant, headers)
        if chosen_links:
            params_list.extend(self._asset_ids_query_values(chosen_links))
        if variant in {"limit_cursor"}:
            params_list.append(("cursor", shell.encoded_id))
        if variant in {"created_after"}:
            params_list.append(("createdAfter", self._created_after_param(matching_data=True)))
        return self._request(
            op_name="get_lookup_shells",
            variant=variant,
            method="GET",
            path="/lookup/shells",
            params=params_list if params_list else None,
            headers=headers,
            shell_id=shell.shell_id if variant != "empty" else None,
        )

    def op_post_lookup_shells_by_asset_link(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or self.rng.choice(["single", "multi", "limit_cursor", "created_after", "empty"])
        shell = self._pick_shell() or self._ensure_shell()
        headers = self._header_for_request("post_lookup_shells_by_asset_link", shell.bpn_values)
        params: dict[str, Any] = {"limit": self.default_limit}
        body = self._choose_lookup_body_links(shell, variant, headers)
        if variant == "limit_cursor":
            params["cursor"] = shell.encoded_id
        elif variant == "created_after":
            params["createdAfter"] = self._created_after_param(matching_data=True)

        return self._request(
            op_name="post_lookup_shells_by_asset_link",
            variant=variant,
            method="POST",
            path="/lookup/shellsByAssetLink",
            params=params if params else None,
            body=body,
            headers=headers,
            shell_id=shell.shell_id if body else None,
        )

    def op_get_lookup_shells_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or ("unknown" if self.rng.random() < 0.06 else "existing")
        shell = self._pick_shell() or self._ensure_shell()
        target = shell.encoded_id if variant == "existing" else encode_url_base64(f"urn:uuid:{uuid.uuid4()}")
        return self._request(
            op_name="get_lookup_shells_by_id",
            variant=variant,
            method="GET",
            path=f"/lookup/shells/{target}",
            headers=self._header_for_request("get_lookup_shells_by_id", shell.bpn_values),
            shell_id=shell.shell_id if variant == "existing" else None,
        )

    def _append_specific_asset_ids(self, shell: ShellRecord, additions: list[dict[str, Any]]) -> None:
        shell.specific_asset_ids.extend(deep_copy_json(additions))
        for entry in additions:
            name = entry.get("name")
            value = entry.get("value")
            if isinstance(name, str) and isinstance(value, str):
                shell.asset_links.append({"name": name, "value": value})
                self.all_asset_link_pool.append({"name": name, "value": value})
            external = entry.get("externalSubjectId")
            if isinstance(external, dict):
                keys = external.get("keys")
                if isinstance(keys, list):
                    for key in keys:
                        if isinstance(key, dict):
                            bpn = key.get("value")
                            if isinstance(bpn, str) and bpn != "PUBLIC_READABLE" and bpn not in shell.bpn_values:
                                shell.bpn_values.append(bpn)

    def op_post_lookup_shells_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or self.rng.choice(["append_links", "plain_only"])
        shell = self._pick_shell() or self._ensure_shell()
        additions: list[dict[str, Any]] = []
        if variant == "plain_only":
            name = self._random_name(["supplierBatch", "traceabilityCode", "materialId"], set(), "plain")
            additions.append({"name": name, "value": self._random_value(name)})
        else:
            name = self._random_name(["supplierBatch", "traceabilityCode", "materialId"], set(), "append")
            bpn = self._random_bpn()
            additions.append(
                {
                    "name": name,
                    "value": self._random_value(name),
                    "externalSubjectId": {
                        "type": "ExternalReference",
                        "keys": [{"type": "GlobalReference", "value": bpn}],
                    },
                }
            )
            if self.rng.random() < 0.5:
                additions.append(
                    {
                        "name": f"{name}Public",
                        "value": self._random_value(f"{name}Public"),
                        "externalSubjectId": {
                            "type": "ExternalReference",
                            "keys": [{"type": "GlobalReference", "value": "PUBLIC_READABLE"}],
                        },
                    }
                )

        entry = self._request(
            op_name="post_lookup_shells_by_id",
            variant=variant,
            method="POST",
            path=f"/lookup/shells/{shell.encoded_id}",
            body=additions,
            headers=self._header_for_request("post_lookup_shells_by_id", shell.bpn_values),
            shell_id=shell.shell_id,
        )
        if isinstance(entry.get("status_code"), int) and 200 <= entry["status_code"] <= 299:
            self._append_specific_asset_ids(shell, additions)
        return entry

    def op_delete_lookup_shells_by_id(self, forced_variant: str | None = None) -> dict[str, Any]:
        variant = forced_variant or ("unknown" if self.rng.random() < 0.2 else "existing")
        shell = self._pick_shell() or self._ensure_shell()
        target = shell.encoded_id if variant == "existing" else encode_url_base64(f"urn:uuid:{uuid.uuid4()}")
        return self._request(
            op_name="delete_lookup_shells_by_id",
            variant=variant,
            method="DELETE",
            path=f"/lookup/shells/{target}",
            headers=self._header_for_request("delete_lookup_shells_by_id", shell.bpn_values),
            shell_id=shell.shell_id if variant == "existing" else None,
        )


def build_arg_parser() -> argparse.ArgumentParser:
    script_dir = Path(__file__).resolve().parent
    default_template = (
        script_dir.parent.parent.parent
        / "internal"
        / "aasregistry"
        / "benchmark_results"
        / "bodies"
        / "testbench.json"
    )
    parser = argparse.ArgumentParser(description="Digital Twin Registry benchmark runner.")
    parser.add_argument("--base-url", default="http://127.0.0.1:6004", help="Digital Twin Registry base URL")
    parser.add_argument(
        "--api-prefix",
        default="/api/v3",
        help="API path prefix added before each endpoint path (use '' to disable)",
    )
    parser.add_argument("--template-json", default=str(default_template), help="Path to testbench JSON template")
    parser.add_argument("--requests", type=int, default=20000, help="Number of weighted benchmark requests")
    parser.add_argument("--seed-shells", type=int, default=500, help="Number of shells to create before benchmark")
    parser.add_argument("--seed", type=int, default=42, help="Random seed for deterministic runs")
    parser.add_argument(
        "--unique-name-values",
        type=int,
        default=20,
        help="Size of the global reusable name/value pool for specificAssetIds",
    )
    parser.add_argument(
        "--unique-bpns",
        type=int,
        default=10,
        help="Number of unique BPN values used by the generator",
    )
    parser.add_argument("--timeout-seconds", type=float, default=10.0, help="HTTP request timeout in seconds")
    parser.add_argument(
        "--default-limit",
        type=int,
        default=1000,
        help="Default limit value for all requests that support the limit query parameter",
    )
    parser.add_argument(
        "--lookup-result-bias",
        type=float,
        default=0.8,
        help="Probability [0..1] to prefer auth-compatible links for lookup-by-asset-link requests",
    )
    parser.add_argument(
        "--weights",
        default=None,
        help=(
            "Override operation weights, e.g. "
            "'post_shell_descriptors=10,put_shell_by_id=5,get_lookup_shells_by_id=40'"
        ),
    )
    parser.add_argument("--output-jsonl", default="runtime_results_dtr.jsonl", help="Per-request output log file")
    parser.add_argument("--summary-json", default="runtime_summary_dtr.json", help="Aggregated summary output file")
    parser.add_argument("--progress-every", type=int, default=5000, help="Print progress every N requests")
    parser.add_argument(
        "--edc-header-mode",
        choices=["random", "always", "never"],
        default="random",
        help="How to include Edc-Bpn header in requests",
    )
    parser.add_argument(
        "--read-identity-weights",
        default="anonymous=20,edc_header=45,admin_token=35",
        help="Identity mix for read-like operations. Example: anonymous=10,edc_header=40,admin_token=50",
    )
    parser.add_argument(
        "--admin-token-url",
        default="http://localhost:8080/realms/basyx/protocol/openid-connect/token",
        help="Keycloak token URL for password grant used by admin identity",
    )
    parser.add_argument(
        "--admin-client-id",
        default="basyx-ui",
        help="OAuth client_id used for admin password grant",
    )
    parser.add_argument(
        "--admin-username",
        default="admin",
        help="Admin username for password grant",
    )
    parser.add_argument(
        "--admin-password",
        default="pwd",
        help="Admin password for password grant",
    )
    parser.add_argument(
        "--bearer-token",
        default=None,
        help="Optional static bearer token (used as admin token and skips token fetch)",
    )
    parser.add_argument(
        "--store-response-body",
        action="store_true",
        help="Store response text preview in JSONL entries",
    )
    parser.add_argument(
        "--store-request-body",
        action="store_true",
        help="Store request body in JSONL entries",
    )
    parser.add_argument(
        "--response-preview-chars",
        type=int,
        default=2000,
        help="Maximum response preview size when --store-response-body is enabled",
    )
    parser.add_argument(
        "--coverage-once",
        action="store_true",
        default=False,
        help="Run each endpoint variant once before weighted benchmark loop",
    )
    return parser


def main() -> int:
    parser = build_arg_parser()
    args = parser.parse_args()
    if args.unique_name_values <= 0:
        raise SystemExit("--unique-name-values must be greater than 0")
    if args.unique_bpns <= 0:
        raise SystemExit("--unique-bpns must be greater than 0")
    if args.default_limit <= 0:
        raise SystemExit("--default-limit must be greater than 0")
    if not 0.0 <= args.lookup_result_bias <= 1.0:
        raise SystemExit("--lookup-result-bias must be between 0.0 and 1.0")

    benchmark = DigitalTwinRegistryBenchmark(args)
    benchmark.ensure_admin_identity_ready()

    output_path = Path(args.output_jsonl)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with output_path.open("w", encoding="utf-8") as log_file:
        print(
            "starting benchmark "
            f"base_url={args.base_url} seed_shells={args.seed_shells} requests={args.requests} seed={args.seed}"
        )
        benchmark.seed_initial_shells(args.seed_shells, log_file)
        print(f"seed phase complete tracked_shells={len(benchmark.shell_records)}")

        if args.coverage_once:
            print("running endpoint coverage pass")
            benchmark.run_coverage_once(log_file)

        benchmark.run_weighted_requests(args.requests, log_file)

    summary_path = Path(args.summary_json)
    summary_path.parent.mkdir(parents=True, exist_ok=True)
    benchmark.write_summary(summary_path)
    print(f"benchmark finished results={output_path} summary={summary_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
