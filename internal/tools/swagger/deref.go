package swagger

import "github.com/getkin/kin-openapi/openapi3"

type SwaggerDerefs struct {
	visitedSchemas map[*openapi3.Schema]bool
}

func NewDerefer() SwaggerDerefs {
	return SwaggerDerefs{
		visitedSchemas: make(map[*openapi3.Schema]bool),
	}
}

func (u *SwaggerDerefs) derefSchemas(schemas openapi3.Schemas) {
	for _, schemaRef := range schemas {
		u.DerefSchemaRef(schemaRef)
	}
}

func (u *SwaggerDerefs) derefSchemaRefs(schemaRefs openapi3.SchemaRefs) {
	for _, schemaRef := range schemaRefs {
		u.DerefSchemaRef(schemaRef)
	}
}

func (u *SwaggerDerefs) DerefSchemaRef(schemaRef *openapi3.SchemaRef) {
	if schemaRef == nil || u.visitedSchemas[schemaRef.Value] {
		return
	}

	u.visitedSchemas[schemaRef.Value] = true

	schemaRef.Ref = ""

	val := schemaRef.Value

	u.derefSchemaRefs(val.OneOf)
	u.derefSchemaRefs(val.OneOf)
	u.derefSchemaRefs(val.AllOf)
	u.DerefSchemaRef(val.Not)
	u.DerefSchemaRef(val.Items)
	u.derefSchemas(val.Properties)
	u.DerefSchemaRef(val.AdditionalProperties.Schema)
}
