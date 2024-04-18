package generator

import (
	"fmt"
	"strings"

	definitionv1alpha1 "github.com/matteogastaldello/swaggergen-provider/apis/definitions/v1alpha1"

	"github.com/krateoplatformops/crdgen"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generation"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generator/text"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

var g *OASSchemaGenerator

type OASSchemaGenerator struct {
	specByteSchema   []byte
	statusByteSchema []byte
	secByteSchema    map[string][]byte
}

func GenerateByteSchemas(doc *libopenapi.DocumentModel[v3.Document], resource definitionv1alpha1.Resource, identifier string) error {
	secByteSchema := make(map[string][]byte)
	var err error
	for secSchemaPair := doc.Model.Components.SecuritySchemes.First(); secSchemaPair != nil; secSchemaPair = secSchemaPair.Next() {
		if !generation.IsValidAuthSchema(secSchemaPair.Value()) {
			continue
		}
		secByteSchema[secSchemaPair.Key()], err = generation.GenerateAuthSchemaFromSecuritySchema(secSchemaPair.Value())
		if err != nil {
			return nil
		}
	}

	specByteSchema := make(map[string][]byte)
	for _, verb := range resource.VerbsDescription {
		if strings.EqualFold(verb.Action, "create") && strings.EqualFold(verb.Method, "post") {
			path := doc.Model.Paths.PathItems.Value(verb.Path)
			if path == nil {
				return fmt.Errorf("path %s not found", verb.Path)
			}
			bodySchema := base.CreateSchemaProxy(&base.Schema{Properties: orderedmap.New[string, *base.SchemaProxy]()})
			if path.Post.RequestBody != nil {
				bodySchema = path.Post.RequestBody.Content.Value("application/json").Schema
			}
			if bodySchema == nil {
				return fmt.Errorf("body schema not found for %s", verb.Path)
			}

			// Add auth schema references to the spec schema
			for key, _ := range secByteSchema {
				bodySchema.Schema().Properties.Set(fmt.Sprintf("%sAuthRef", text.CapitaliseFirstLetter(key)),
					base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}))
			}

			schema, err := bodySchema.BuildSchema()
			if err != nil {
				return fmt.Errorf("building schema for %s: %w", verb.Path, err)
			}
			for _, param := range path.Post.Parameters {
				schema.Properties.Set(param.Name, param.Schema)
			}
			byteSchema, err := generation.GenerateJsonSchemaFromSchemaProxy(base.CreateSchemaProxy(schema))
			if err != nil {
				return err
			}
			specByteSchema[resource.Kind] = byteSchema
		}
	}

	var statusByteSchema []byte

	// Create an ordered property map
	propMap := orderedmap.New[string, *base.SchemaProxy]()

	// Add a field named "exampleField" of type string
	propMap.Set(identifier, base.CreateSchemaProxy(&base.Schema{
		Type: []string{"string"},
	}))

	// Create a schema proxy with the properties map
	schemaProxy := base.CreateSchemaProxy(&base.Schema{
		Type:       []string{"object"},
		Properties: propMap,
	})

	statusSchema, err := schemaProxy.BuildSchema()
	if err != nil {
		return fmt.Errorf("building status schema for %s: %w", identifier, err)
	}

	statusByteSchema, err = generation.GenerateJsonSchemaFromSchemaProxy(base.CreateSchemaProxy(statusSchema))
	if err != nil {
		return err
	}

	g = &OASSchemaGenerator{
		specByteSchema:   specByteSchema[resource.Kind],
		statusByteSchema: statusByteSchema,
		secByteSchema:    secByteSchema,
	}

	return nil
}

func OASSpecJsonSchemaGetter(doc *libopenapi.DocumentModel[v3.Document], resource definitionv1alpha1.Resource) crdgen.JsonSchemaGetter {
	return &oasSpecJsonSchemaGetter{
		doc:      doc,
		resource: resource,
	}
}

var _ crdgen.JsonSchemaGetter = (*oasSpecJsonSchemaGetter)(nil)

type oasSpecJsonSchemaGetter struct {
	doc      *libopenapi.DocumentModel[v3.Document]
	resource definitionv1alpha1.Resource
}

// func (g *oasSpecJsonSchemaGetter) Get() ([]byte, error) {
// 	byteSchema := make(map[string][]byte)
// 	for _, verb := range g.resource.VerbsDescription {
// 		if strings.EqualFold(verb.Action, "create") && strings.EqualFold(verb.Method, "post") {
// 			path := g.doc.Model.Paths.PathItems.Value(verb.Path)
// 			if path == nil {
// 				return nil, fmt.Errorf("path %s not found", verb.Path)
// 			}
// 			bodySchema := base.CreateSchemaProxy(&base.Schema{Properties: orderedmap.New[string, *base.SchemaProxy]()})
// 			if path.Post.RequestBody != nil {
// 				bodySchema = path.Post.RequestBody.Content.Value("application/json").Schema //path.Post.RequestBody.Value.Content.Get("application/json").Schema
// 			}
// 			if bodySchema == nil {
// 				return nil, fmt.Errorf("body schema not found for %s", verb.Path)
// 			}
// 			schema, err := bodySchema.BuildSchema()
// 			if err != nil {
// 				return nil, fmt.Errorf("building schema for %s: %w", verb.Path, err)
// 			}
// 			for _, param := range path.Post.Parameters {
// 				schema.Properties.Set(param.Name, param.Schema)
// 			}
// 			byteSchema[g.resource.Kind], err = generation.GenerateJsonSchemaFromSchemaProxy(base.CreateSchemaProxy(schema))
// 			if err != nil {
// 				return nil, err
// 			}
// 		}
// 	}
// 	return byteSchema[g.resource.Kind], nil
// }

func (a *oasSpecJsonSchemaGetter) Get() ([]byte, error) {
	return g.specByteSchema, nil
}

// func OASStatusJsonSchemaGetter(doc *libopenapi.DocumentModel[v3.Document], resource definitionv1alpha1.Resource) crdgen.JsonSchemaGetter {
// 	return &oasStatusJsonSchemaGetter{
// 		doc:      doc,
// 		resource: resource,
// 	}
// }

// var _ crdgen.JsonSchemaGetter = (*oasStatusJsonSchemaGetter)(nil)

// type oasStatusJsonSchemaGetter struct {
// 	doc      *libopenapi.DocumentModel[v3.Document]
// 	resource definitionv1alpha1.Resource
// }

// func (g *oasStatusJsonSchemaGetter) Get() ([]byte, error) {
// 	var responseByteSchema []byte

// 	for _, verb := range g.resource.VerbsDescription {
// 		if strings.EqualFold(verb.Action, "create") && strings.EqualFold(verb.Method, "post") {
// 			path := g.doc.Model.Paths.PathItems.Value(verb.Path)
// 			if path == nil {
// 				return nil, fmt.Errorf("path %s not found", verb.Path)
// 			}
// 			// Now, we need to get the response schema for the status
// 			responseSchemaProxy := base.CreateSchemaProxy(&base.Schema{Properties: orderedmap.New[string, *base.SchemaProxy]()})
// 			if path.Post.Responses.FindResponseByCode(200) != nil {
// 				responseSchemaProxy = path.Post.Responses.FindResponseByCode(200).Content.Value("application/json").Schema
// 			}
// 			if responseSchemaProxy == nil {
// 				return nil, fmt.Errorf("response schema proxy not found for %s", verb.Path)
// 			}
// 			responseSchema, err := responseSchemaProxy.BuildSchema()
// 			if err != nil {
// 				return nil, fmt.Errorf("building response schema for %s: %w", verb.Path, err)
// 			}

// 			responseByteSchema, err = generation.GenerateJsonSchemaFromSchemaProxy(base.CreateSchemaProxy(responseSchema))
// 			if err != nil {
// 				return nil, err
// 			}
// 		}
// 	}

// 	return responseByteSchema, nil
// }

func OASStatusJsonSchemaGetter(doc *libopenapi.DocumentModel[v3.Document], identifier string) crdgen.JsonSchemaGetter {
	return &oasStatusJsonSchemaGetter{
		doc:        doc,
		identifier: identifier,
	}
}

var _ crdgen.JsonSchemaGetter = (*oasStatusJsonSchemaGetter)(nil)

type oasStatusJsonSchemaGetter struct {
	doc        *libopenapi.DocumentModel[v3.Document]
	identifier string
}

// func (g *oasStatusJsonSchemaGetter) Get() ([]byte, error) {
// 	var responseByteSchema []byte

// 	// Create an ordered property map
// 	propMap := orderedmap.New[string, *base.SchemaProxy]()

// 	// Add a field named "exampleField" of type string
// 	propMap.Set(g.identifier, base.CreateSchemaProxy(&base.Schema{
// 		Type: []string{"string"},
// 	}))

// 	// Create a schema proxy with the properties map
// 	schemaProxy := base.CreateSchemaProxy(&base.Schema{
// 		Type:       []string{"object"},
// 		Properties: propMap,
// 	})

// 	responseSchema, err := schemaProxy.BuildSchema()
// 	if err != nil {
// 		return nil, fmt.Errorf("building response schema for %s: %w", g.identifier, err)
// 	}

// 	responseByteSchema, err = generation.GenerateJsonSchemaFromSchemaProxy(base.CreateSchemaProxy(responseSchema))
// 	if err != nil {
// 		return nil, err
// 	}

// 	return responseByteSchema, nil
// }

func (a *oasStatusJsonSchemaGetter) Get() ([]byte, error) {
	return g.statusByteSchema, nil
}

func OASAuthJsonSchemaGetter(secSchema *v3.SecurityScheme, resource definitionv1alpha1.Resource) crdgen.JsonSchemaGetter {
	return &oasAuthJsonSchemaGetter{
		secSchema: secSchema,
		resource:  resource,
	}
}

var _ crdgen.JsonSchemaGetter = (*oasAuthJsonSchemaGetter)(nil)

type oasAuthJsonSchemaGetter struct {
	secSchema *v3.SecurityScheme
	resource  definitionv1alpha1.Resource
}

func (g *oasAuthJsonSchemaGetter) Get() ([]byte, error) {
	byteSchema, err := generation.GenerateAuthSchemaFromSecuritySchema(g.secSchema)
	if err != nil {
		return nil, nil
	}
	return byteSchema, nil
}

var _ crdgen.JsonSchemaGetter = (*staticJsonSchemaGetter)(nil)

func StaticJsonSchemaGetter() crdgen.JsonSchemaGetter {
	return &staticJsonSchemaGetter{}
}

type staticJsonSchemaGetter struct {
}

func (f *staticJsonSchemaGetter) Get() ([]byte, error) {
	return nil, nil
}
