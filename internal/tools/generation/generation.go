package generation

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/invopop/jsonschema"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generator/text"
)

func GenerateJsonSchema(doc *openapi3.T) (map[string][]byte, error) {
	components := doc.Components
	byteMap := make(map[string][]byte)
	var err error
	for key, schema := range components.Schemas {
		for propName, property := range schema.Value.Properties {
			if property.Ref != "" {
				schema.Value.Properties[text.FirstToLower(propName)].Ref = ""
			}
			if property.Value.Type == "array" {
				if property.Value.Items.Ref != "" {
					schema.Value.Properties[text.FirstToLower(propName)].Value.Items.Ref = ""
				}
			}
		}
		byteMap[key], err = schema.Value.MarshalJSON()

		// fmt.Println("\nKEY: ", key, "\n")
		// fmt.Println(string(byteMap[key]))
		// fmt.Println("\n\n")

		if err != nil {
			return nil, err
		}
	}

	return byteMap, nil
}

type BasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type BearerAuth struct {
	Token string `json:"token"`
}

func GenerateAuthSchema(doc *openapi3.T) (map[string][]byte, error) {
	securitySchema := doc.Components.SecuritySchemes
	var err error
	byteSchema := make(map[string][]byte)
	for _, schema := range securitySchema {
		if schema.Value.Type == "http" && schema.Value.Scheme == "basic" {
			authSchema := jsonschema.Reflect(&BasicAuth{})
			byteSchema[schema.Value.Scheme], err = authSchema.Definitions["BasicAuth"].MarshalJSON()
			if err != nil {
				return nil, err
			}
		} else if schema.Value.Type == "http" && schema.Value.Scheme == "bearer" {
			authSchema := jsonschema.Reflect(&BearerAuth{})
			byteSchema[schema.Value.Scheme], err = authSchema.Definitions["BearerAuth"].MarshalJSON()
			if err != nil {
				return nil, err
			}
		}
	}

	return byteSchema, nil
}
