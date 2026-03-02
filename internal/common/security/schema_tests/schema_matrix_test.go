package schema_tests

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"
)

type schemaCase struct {
	name    string
	payload string
	valid   bool
}

func TestSchemaValidationMatrix(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}

	schemaPath := filepath.Join(repoRoot, "internal", "common", "security", "schema_tests", "schema.json")
	schemaLoader, err := buildCompatibleSchemaLoader(schemaPath)
	if err != nil {
		t.Fatalf("failed to load schema: %v", err)
	}

	cases := []schemaCase{
		{
			name: "valid query minimal",
			payload: `{
  "Query": {
    "$condition": {"$boolean": true}
  }
}`,
			valid: true,
		},
		{
			name: "valid query full",
			payload: `{
  "Query": {
    "$select": "id",
    "$condition": {
      "$and": [
        {"$eq": [{"$attribute": {"CLAIM": "role"}}, {"$strVal": "admin"}]},
        {"$boolean": true}
      ]
    },
    "$filters": [
      {
        "$fragment": "$aasdesc#idShort",
        "$condition": {
          "$contains": [{"$field": "$aasdesc#idShort"}, {"$strVal": "AAS"}]
        }
      }
    ]
  }
}`,
			valid: true,
		},
		{
			name: "invalid query missing condition",
			payload: `{
  "Query": {
    "$select": "id"
  }
}`,
			valid: false,
		},
		{
			name: "invalid query select pattern",
			payload: `{
  "Query": {
    "$select": "identifier",
    "$condition": {"$boolean": true}
  }
}`,
			valid: false,
		},
		{
			name: "valid access rule refs",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "USEFORMULA": "allow"
      }
    ]
  }
}`,
			valid: true,
		},
		{
			name: "valid access rule inline",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "ACL": {
          "ATTRIBUTES": [{"GLOBAL": "ANONYMOUS"}],
          "RIGHTS": ["READ"],
          "ACCESS": "ALLOW"
        },
        "OBJECTS": [{"ROUTE": "/description"}],
        "FORMULA": {"$boolean": true}
      }
    ]
  }
}`,
			valid: true,
		},
		{
			name: "valid access rule fragment filter",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "USEFORMULA": "allow",
        "FRAGMENT": "$aasdesc#idShort",
        "FILTER": {
          "$starts-with": [{"$field": "$aasdesc#idShort"}, {"$strVal": "A"}]
        }
      }
    ]
  }
}`,
			valid: true,
		},
		{
			name: "valid access rule fragment usefilter",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "USEFORMULA": "allow",
        "FRAGMENT": "$aasdesc#idShort",
        "USEFILTER": "namedFilter"
      }
    ]
  }
}`,
			valid: true,
		},
		{
			name: "valid filterlist with inline filter",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "USEFORMULA": "allow",
        "FILTERLIST": [
          {
            "FRAGMENT": "$aasdesc#displayName",
            "FILTER": {"$boolean": true}
          }
        ]
      }
    ]
  }
}`,
			valid: true,
		},
		{
			name: "valid filterlist with usefilter",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "USEFORMULA": "allow",
        "FILTERLIST": [
          {
            "FRAGMENT": "$aasdesc#displayName",
            "USEFILTER": "namedFilter"
          }
        ]
      }
    ]
  }
}`,
			valid: true,
		},
		{
			name: "invalid fragment without filter",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "USEFORMULA": "allow",
        "FRAGMENT": "$aasdesc#idShort"
      }
    ]
  }
}`,
			valid: false,
		},
		{
			name: "invalid filterlist item missing filter",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "USEFORMULA": "allow",
        "FILTERLIST": [
          {
            "FRAGMENT": "$aasdesc#displayName"
          }
        ]
      }
    ]
  }
}`,
			valid: false,
		},
		{
			name: "invalid acl and useacl together",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "ACL": {
          "ATTRIBUTES": [{"GLOBAL": "ANONYMOUS"}],
          "RIGHTS": ["READ"],
          "ACCESS": "ALLOW"
        },
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "USEFORMULA": "allow"
      }
    ]
  }
}`,
			valid: false,
		},
		{
			name: "invalid model field pattern",
			payload: `{
  "AllAccessPermissionRules": {
    "rules": [
      {
        "USEACL": "read_public",
        "USEOBJECTS": ["desc"],
        "FORMULA": {
          "$eq": [
            {"$field": "$bd#idShort"},
            {"$strVal": "x"}
          ]
        }
      }
    ]
  }
}`,
			valid: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := gojsonschema.Validate(schemaLoader, gojsonschema.NewStringLoader(testCase.payload))
			if err != nil {
				t.Fatalf("validation failed: %v", err)
			}
			if result.Valid() != testCase.valid {
				var messages []string
				for _, validationErr := range result.Errors() {
					messages = append(messages, validationErr.String())
				}
				t.Fatalf("expected valid=%t, got valid=%t; errors: %s", testCase.valid, result.Valid(), strings.Join(messages, " | "))
			}
		})
	}
}
