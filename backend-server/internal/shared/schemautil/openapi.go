package schemautil

import (
	"github.com/bytedance/sonic"
	"github.com/eino-contrib/jsonschema"
	"github.com/getkin/kin-openapi/openapi3"
)

// OpenAPIToJSONSchema converts an OpenAPI schema into a jsonschema.Schema so it
// can be supplied to eino's schema helpers.
func OpenAPIToJSONSchema(openAPISchema *openapi3.Schema) (*jsonschema.Schema, error) {
	if openAPISchema == nil {
		return nil, nil
	}

	data, err := sonic.Marshal(openAPISchema)
	if err != nil {
		return nil, err
	}

	jsSchema := &jsonschema.Schema{}
	if err := sonic.Unmarshal(data, jsSchema); err != nil {
		return nil, err
	}

	return jsSchema, nil
}
