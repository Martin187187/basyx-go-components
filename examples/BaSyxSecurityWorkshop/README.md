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
- `examples/BaSyxSecurityWorkshop/docker_compose/access_rules/access-rules.json`  
  The active model file mounted into the registry container (`/config/access_rules/access-rules.json`).

---

## 3. Prerequisites

You need the following installed on your machine:
1. **Podman or Docker** (compose plugin available). The instructions use `podman compose`, but you can replace it with `docker compose`.
2. **Go toolchain** (to run the Go tests).

---

## 4. The Running Stack (Compose) and How to Start It

What the compose stack contains:
- **Postgres** on `localhost:5432` (db: `basyxTestDB`, user: `admin`, password: `admin123`).
- **Keycloak** on `localhost:8080` with the imported realm `basyx`.
- **AAS Registry** on `localhost:6004` that mounts your access rules from `docker_compose/access_rules/access-rules.json`.

Start the stack (from `examples/BaSyxSecurityWorkshop`):
```
docker compose up -d --build
```
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
        "FORMULA": { "$boolean: true" }
      }
    ]
  }
}

```

## 6. Easy Task (Business Case + How-To)

**Business case:**  
- allow read access for Shelldescriptor id: "http://martin.de" for everyone.

**What to change:**
1. Open and edit `docker_compose/access_rules/access-rules.json`.
2. Restart registry: `docker compose restart workshop_aas-registry-security`


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
- Everybody can read descriptors that have "PUBLIC_READABLE" in $aasdesc#specificAssetIds[].externalSubjectId.keys[].value.
- Viewers can use /shell-descriptors/{id}.
- Viewers with clear>1  can read all shell-descriptors.
- Admins can CREATE, UPDATE, DELETE and READ all shell-descriptors

**What to change:**
**What to change:**
1. Open and edit `docker_compose/access_rules/access-rules.json`.
2. Restart registry: `docker compose restart workshop_aas-registry-security`

---

## 10. Materialization

You can define 