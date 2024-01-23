package generation

import (
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

func GenerateJsonSchema(doc *openapi3.T) (map[string][]byte, error) {
	components := doc.Components
	byteMap := make(map[string][]byte)
	var err error
	for key, schema := range components.Schemas {
		for propName, property := range schema.Value.Properties {
			if property.Ref != "" {
				schema.Value.Properties[strings.ToLower(propName)].Ref = ""
			}
			if property.Value.Type == "array" {
				if property.Value.Items.Ref != "" {
					schema.Value.Properties[strings.ToLower(propName)].Value.Items.Ref = ""
				}
			}
		}
		byteMap[key], err = schema.Value.MarshalJSON()
		if err != nil {
			return nil, err
		}
	}
	return byteMap, nil
}
