#!/bin/sh
set -e

track="${1:-}"
case "$track" in
  easy)   test_filter="TestWorkshopEasy"; rule_file="easy.json" ;;
  medium) test_filter="TestWorkshopMedium"; rule_file="medium.json" ;;
  hard)   test_filter="TestWorkshopIntegration"; rule_file="hard.json" ;;
  *)
    echo "Usage: $0 {easy|medium|hard}" >&2
    exit 1
    ;;
esac

rule_src="/workspace/examples/BaSyxSecurityWorkshop/access_rules/$rule_file"
rule_dst="/workspace/examples/BaSyxSecurityWorkshop/access_rules/access-rules.json"
if [ ! -f "$rule_src" ]; then
  echo "Rule file missing: $rule_src" >&2
  exit 1
fi
cp "$rule_src" "$rule_dst"

if ! command -v curl >/dev/null 2>&1; then
  apt-get update -y >/dev/null
  apt-get install -y curl >/dev/null
fi

restart_registry() {
  ok=0
  for sock in /var/run/docker.sock /run/podman/podman.sock; do
    if [ -S "$sock" ]; then
      echo "Restarting registry via $sock ..."
      if curl --unix-socket "$sock" -s -X POST http://localhost/containers/aas-registry-workshop/restart >/dev/null; then
        ok=1
        break
      else
        echo "Registry restart failed via $sock" >&2
      fi
    fi
  done
  if [ $ok -eq 0 ]; then
    echo "Could not restart registry from runner; restart manually if rules changed." >&2
    exit 1
  fi
}

wait_for_registry() {
  url="http://aas-registry-workshop:5004/health"
  attempts=60
  while [ "$attempts" -gt 0 ]; do
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "$url" || true)
    if [ "$code" -ne 000 ] && [ "$code" -lt 500 ]; then
      echo "Registry healthy (status $code)"
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 2
  done
  echo "Registry did not become healthy in time" >&2
  exit 1
}

restart_registry
wait_for_registry

export WORKSHOP_BASE_URL="http://aas-registry-workshop:5004"
export WORKSHOP_TOKEN_URL="${WORKSHOP_TOKEN_URL:-http://keycloak-workshop:8080/realms/basyx/protocol/openid-connect/token}"
export WORKSHOP_DB_HOST="${WORKSHOP_DB_HOST:-db-workshop}"
export WORKSHOP_DB_PORT="${WORKSHOP_DB_PORT:-5432}"
export WORKSHOP_DB_USER="${WORKSHOP_DB_USER:-admin}"
export WORKSHOP_DB_PASSWORD="${WORKSHOP_DB_PASSWORD:-admin123}"
export WORKSHOP_DB_NAME="${WORKSHOP_DB_NAME:-basyxTestDB}"
export GOTOOLCHAIN="local"

cd /workspace
echo "Running Go tests: $test_filter"
go test -v ./examples/BaSyxSecurityWorkshop -run "$test_filter" -count=1
