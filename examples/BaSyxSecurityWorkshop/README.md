# BaSyx Security Workshop — Step‑by‑Step Tutorial

This tutorial walks you through designing, applying, and testing Access Rule Models for the AAS Registry. It is organized in three tracks (easy, medium, hard) and explains every step in detail so you can learn by doing. The document is intentionally verbose and self‑contained. If you already know a section, feel free to skim it.

---

## Table of Contents
1. Purpose and Learning Goals
2. Repository Layout for the Workshop
3. Prerequisites
4. The Running Stack (Compose) and How to Start It
5. Users, Credentials, and Claims (Keycloak Realm)
6. Access Rule Model Basics
7. Access Rule Materialization (Hard Track Concept)
8. Editing and Applying Models
9. Easy Track
10. Medium Track
11. Hard Track
12. Test Suites and How to Run Them
13. Troubleshooting and Tips
14. FAQ
15. Glossary

---

## 1. Purpose and Learning Goals
- Learn how BaSyx ABAC access rules are expressed in JSON.
- Understand how claims (e.g., `role`, `clear`) map to access decisions.
- Practice with three difficulty levels:
  - **Easy:** Minimal rules, simple `CLAIM` == `strVal` comparisons.
  - **Medium:** Multiple rules, create/read/delete flows.
  - **Hard:** Full integration suite (reuses the original registry security tests), plus a glimpse of materialization.
- Be able to restart the registry with a new rule set and verify behavior via tests.

---

## 2. Repository Layout for the Workshop

Key paths you will touch:
- `examples/BaSyxSecurityWorkshop/access_rules/easy.json`  
  Rule set for the easy track.
- `examples/BaSyxSecurityWorkshop/access_rules/medium.json`  
  Rule set for the medium track.
- `examples/BaSyxSecurityWorkshop/access_rules/hard.json`  
  Rule set for the hard track.
- `examples/BaSyxSecurityWorkshop/access_rules/access-rules.json`  
  The active rule file mounted into the single registry container (`/config/access_rules/access-rules.json`, port 6004). The runner copies the chosen track’s file here before each test run.

---

## 3. Prerequisites

You need the following installed on your machine:
1. **Podman or Docker** (compose plugin available). The commands below work with either; replace `docker` with `podman` if you prefer Podman.
2. **No local Go toolchain needed.** Tests run inside the provided `workshop-test` container.

---

## 4. The Running Stack (Compose) and How to Start It

What the compose stack contains (single registry reused by all tracks):
- **Postgres** on `localhost:5432` (db: `basyxTestDB`, user: `admin`, password: `admin123`).
- **Keycloak** on `localhost:8080` with the imported realm `basyx`.
- **AAS Registry** on `localhost:6004` mounting `access_rules/access-rules.json` (the runner copies the selected track’s rules here before each test run).
- **Test runner** container that executes the Go suites for you.

Start the stack once (from `examples/BaSyxSecurityWorkshop/server_secured`):
```
docker compose -f docker_compose.yml up -d --build
```
Use `podman compose -f docker_compose.yml` instead of `docker compose -f docker_compose.yml` if you are on Podman.
---

## 5. Easy Task Foundations (Definitions + Examples)

Before the first hands-on task, here are the minimal concepts you need for the easy level.

### 5.1 Root
`AllAccessPermissionRules` is the root element and can have multiple rules. The core idea is that per default everything is denied. By defining more and more rules you can give access to certain objects.
```
{
  "rules": []
}
```
### 5.2 ACL
Each rule has an `ACL`: An ACL mus have `ACCESS` and `RIGHTS` fields. `ACCESS` can be `ALLOW` (rule is active) or `DISABLED` (rule is disabled). `RIGHTS` can be `READ`, `UPDATE`, `CREATE`, `DELETE` for example. `{ "GLOBAL": "ANONYMOUS" }` gives access to everyone
```
{
  "ACCESS": "ALLOW",
  "RIGHTS": ["READ"],
  "attributes": [{ "GLOBAL": "ANONYMOUS" }]
}
```

### 5.3 OBJECT
Each rule has an `OBJECT`: An Object is what the rule gives access to. You can define multiple Objects per rule. It gives access to all objects (union).
```
{ "ROUTE": "/route" }
```

### 5.4 FORMULA
Each rule has to have a `FORMULA`.
```
{ "$boolean: true" }
```

### 5.5 complete Access Rule Example

This rule gives read access to /route for everone.
```
{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "ACL": {
          "ACCESS": "ALLOW",
          "RIGHTS": ["READ"],
          "attributes": [{ "GLOBAL": "ANONYMOUS" }]
        },
        "OBJECTS": [{ "ROUTE": "/route" }],
        "FORMULA": { "$boolean": true }
      }
    ]
  }
}

```

## 6. Easy Task (Business Case + How-To)

**Business case:**  
- allow read access for Shelldescriptor Get by id: "http://martin.de" for everyone (not list endpoint).

**What to change:**
1. Open and edit `access_rules/easy.json`.
2. Run the easy tests (this copies the rule into the active file and restarts the single registry if Docker/Podman is available inside the runner):  
   `docker compose run --rm workshop-test easy`


## 7. Users, Credentials, and Claims (Keycloak Realm)

The realm provides these users by default (password for all below: `pwd` unless noted):
- `admin` — claim `role=admin`, `clear=10`.
- `usera` — claim `role=viewer`.
- `userb` — claim `role=viewer`, `clear=2`.
- `userx` — claim `role=editor`.
- `usery` — claim `role=editor`, `clear=2`.

Claims visible to rules:
- `role`: one of `admin`, `viewer`, `editor`.
- (For the hard suite, additional claims like `clear` may exist; see the realm export for more.)

---

## 8. Medium Task Foundations (Definitions + Examples)

### 8.1 ACL
`ACL` can have a list of attributes with claims. These are attributes that have to exist in a JWT token for the certain rule.
```
{
  "ACCESS": "ALLOW",
  "RIGHTS": ["READ"],
  "attributes": [ { "CLAIM": "role" }, { "CLAIM": "clear" } ]
}
```
### 8.2 OBJECT
You can define `OBJECT` more than just Route Objects. A Descritpor Objects gives you to all endpoints that are relevant for that object. 
You can give access too descriptor or to descritpors with specific id:
```
{ "DESCRIPTOR": "$aasdesc(\"https://example.org/X\")" } 
```
```
{ "DESCRIPTOR": "$aasdesc(\"*\") } 
```
### 8.3 FORMULA
`FORMULA` can evaluate attributes and other fields in a rule. Fields are values in the database. Attibutes are claims.
```
{ 
  "$and": [
    {"$eq": [ {"$attribute": {"CLAIM": "role"}}, {"$strVal": "viewer"}]},
    {"$eq": [ {"$field": "$aasdesc#specificAssetIds[].name"}, {"$strVal": "customerPartId"}]}
  ] 
}
```
```
{"$eq": [ {"$field": "$aasdesc#specificAssetIds[].externalSubjectId.keys[].value"}, {"$strVal": "PUBLIC_READABLE"}]}
```

## 9. Medium Task (Business Case + How-To)

**Business case:**  
- Anonymous can read descriptors tagged PUBLIC_READABLE.
- Viewers can read descriptors that have interface `AAS-3.0` (secure endpoints), plus public ones; no writes.
- Editors or admins with `clear >= 2` can read/write descriptors with interface `AAS-3.0`.
- Admins or editors with `clear >= 10` can read/write everything.

**What to change:**
1. Open and edit `access_rules/medium.json`.
2. Run the medium tests:  
   `docker compose run --rm workshop-test medium`

**Exercise:** Implement the rules so that the above behaviors hold, using claims (`role`, `clear`), descriptor interfaces, and the PUBLIC_READABLE tag. Test with anonymous, viewer (usera), editor (usery), and admin.

---

## 10. Hard Task (Integration Suite)

For the hard track you reuse the full integration suite.

**What to change:**
1. Open and edit `access_rules/hard.json`.
2. Run the hard tests (integration suite):  
   `docker compose run --rm workshop-test hard`

---

## 12. Test Suites and How to Run Them

- Start everything once (from `server_secured`): `docker compose up -d --build`  
  (use `podman compose -f docker_compose.yml` on Podman).
- Easy: `docker compose -f docker_compose.yml run --rm workshop-test easy`
- Medium: `docker compose -f docker_compose.yml run --rm workshop-test medium`
- Hard: `docker compose -f docker_compose.yml run --rm workshop-test hard`

The runner copies the selected rule file into the active `access-rules.json`, restarts the registry (if Docker/Podman is available inside the runner), waits for health, and then executes the tests. If restart is not possible from inside the runner, restart the registry manually before running the command.

---

## 10. Materialization

You can define 
