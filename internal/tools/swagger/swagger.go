package swagger

import (
	"log"

	"github.com/getkin/kin-openapi/openapi3"
)

func LoadSchema(path string) (*openapi3.T, error) {
	// Load the OpenAPI 3.0 spec fileg
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		log.Fatalf("Failed to load OpenAPI spec: %v", err)
	}
	return doc, nil
}
