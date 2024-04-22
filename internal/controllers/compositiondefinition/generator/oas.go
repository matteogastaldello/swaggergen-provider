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

// GenerateByteSchemas generates the byte schemas for the spec, status and auth schemas. Returns a fatal error and a list of generic errors.
func GenerateByteSchemas(doc *libopenapi.DocumentModel[v3.Document], resource definitionv1alpha1.Resource, identifier string) (fatalError error, errors []error) {
	secByteSchema := make(map[string][]byte)
	var err error
	for secSchemaPair := doc.Model.Components.SecuritySchemes.First(); secSchemaPair != nil; secSchemaPair = secSchemaPair.Next() {
		authSchemaName, err := generation.GenerateAuthSchemaName(secSchemaPair.Value())
		if err != nil {
			errors = append(errors, err)
			continue
		}

		secByteSchema[authSchemaName], err = generation.GenerateAuthSchemaFromSecuritySchema(secSchemaPair.Value())
		if err != nil {
			errors = append(errors, err)
			continue
		}
	}

	specByteSchema := make(map[string][]byte)
	for _, verb := range resource.VerbsDescription {
		if strings.EqualFold(verb.Action, "create") && strings.EqualFold(verb.Method, "post") {
			path := doc.Model.Paths.PathItems.Value(verb.Path)
			if path == nil {
				return fmt.Errorf("path %s not found", verb.Path), errors
			}
			bodySchema := base.CreateSchemaProxy(&base.Schema{Properties: orderedmap.New[string, *base.SchemaProxy]()})
			if path.Post.RequestBody != nil {
				bodySchema = path.Post.RequestBody.Content.Value("application/json").Schema
			}
			if bodySchema == nil {
				return fmt.Errorf("body schema not found for %s", verb.Path), errors
			}

			// Add auth schema references to the spec schema
			bodySchema.Schema().Properties.Set("authenticationRefs", base.CreateSchemaProxy(&base.Schema{
				Type:        []string{"object"},
				Description: "AuthenticationRefs represent the reference to a CR containing the authentication information. One authentication method must be set."}))
			bodySchema.Schema().Required = append(bodySchema.Schema().Required, "authenticationRefs")
			for key, _ := range secByteSchema {
				authSchemaProxy := bodySchema.Schema().Properties.Value("authenticationRefs")
				if authSchemaProxy == nil {
					return fmt.Errorf("authenticationRefs schema not found for %s", verb.Path), errors
				}

				// Ensure authSchemaProxy.Schema().Properties is initialized
				if authSchemaProxy.Schema().Properties == nil {
					authSchemaProxy.Schema().Properties = orderedmap.New[string, *base.SchemaProxy]()
				}
				authSchemaProxy.Schema().Properties.Set(fmt.Sprintf("%sRef", text.FirstToLower(key)),
					base.CreateSchemaProxy(&base.Schema{Type: []string{"string"}}))
			}

			schema, err := bodySchema.BuildSchema()
			if err != nil {
				return fmt.Errorf("building schema for %s: %w", verb.Path, err), errors
			}

			// for secSchemaPair := doc.Model.Components.SecuritySchemes.First(); secSchemaPair != nil; secSchemaPair = secSchemaPair.Next() {
			for _, verb := range resource.VerbsDescription {
				path := doc.Model.Paths.PathItems.Value(verb.Path)
				ops := path.GetOperations()
				if ops == nil {
					continue
				}
				for op := ops.First(); op != nil; op = op.Next() {
					for _, param := range op.Value().Parameters {
						if _, ok := schema.Properties.Get(param.Name); ok {
							errors = append(errors, fmt.Errorf("parameter %s already exists in schema", param.Name))
							continue
						}
						schema.Properties.Set(param.Name, param.Schema)
						schemaProxyParam := schema.Properties.Value(param.Name)
						schemaParam, err := schemaProxyParam.BuildSchema()
						if err != nil {
							return fmt.Errorf("building schema for %s: %w", verb.Path, err), errors
						}
						schemaParam.Description = fmt.Sprintf("PARAMETER: %s, VERB: %s - %s", param.In, text.CapitaliseFirstLetter(op.Key()), param.Description)
					}
				}
			}
			byteSchema, err := generation.GenerateJsonSchemaFromSchemaProxy(base.CreateSchemaProxy(schema))
			if err != nil {
				return err, errors
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
		return fmt.Errorf("building status schema for %s: %w", identifier, err), errors
	}

	statusByteSchema, err = generation.GenerateJsonSchemaFromSchemaProxy(base.CreateSchemaProxy(statusSchema))
	if err != nil {
		return err, errors
	}

	g = &OASSchemaGenerator{
		specByteSchema:   specByteSchema[resource.Kind],
		statusByteSchema: statusByteSchema,
		secByteSchema:    secByteSchema,
	}

	return nil, errors
}

func OASSpecJsonSchemaGetter() crdgen.JsonSchemaGetter {
	return &oasSpecJsonSchemaGetter{}
}

var _ crdgen.JsonSchemaGetter = (*oasSpecJsonSchemaGetter)(nil)

type oasSpecJsonSchemaGetter struct {
}

func (a *oasSpecJsonSchemaGetter) Get() ([]byte, error) {
	// fmt.Println("specByteSchema", string(g.specByteSchema))
	return g.specByteSchema, nil
}

func OASStatusJsonSchemaGetter() crdgen.JsonSchemaGetter {
	return &oasStatusJsonSchemaGetter{}
}

var _ crdgen.JsonSchemaGetter = (*oasStatusJsonSchemaGetter)(nil)

type oasStatusJsonSchemaGetter struct {
}

func (a *oasStatusJsonSchemaGetter) Get() ([]byte, error) {
	return g.statusByteSchema, nil
}

func OASAuthJsonSchemaGetter(secSchemaName string) crdgen.JsonSchemaGetter {
	return &oasAuthJsonSchemaGetter{
		secSchemaName: secSchemaName,
	}
}

var _ crdgen.JsonSchemaGetter = (*oasAuthJsonSchemaGetter)(nil)

type oasAuthJsonSchemaGetter struct {
	secSchemaName string
}

func (a *oasAuthJsonSchemaGetter) Get() ([]byte, error) {

	return g.secByteSchema[a.secSchemaName], nil
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
