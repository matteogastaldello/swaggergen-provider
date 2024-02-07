package definition

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"k8s.io/client-go/tools/record"

	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/event"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"
	definitionv1alpha1 "github.com/matteogastaldello/swaggergen-provider/apis/definitions/v1alpha1"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"

	"github.com/matteogastaldello/swaggergen-provider/internal/tools/crds"
	generation "github.com/matteogastaldello/swaggergen-provider/internal/tools/generation"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generator/code"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generator/text"
)

const (
	errNotDefinition = "managed resource is not a Definition"
	labelKeyGroup    = "krateo.io/crd-group"
	labelKeyVersion  = "krateo.io/crd-version"
	labelKeyResource = "krateo.io/crd-resource"
)

func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := reconciler.ControllerName(definitionv1alpha1.DefinitionGroupKind)

	log := o.Logger.WithValues("controller", name)

	recorder := mgr.GetEventRecorderFor(name)

	r := reconciler.NewReconciler(mgr,
		resource.ManagedKind(definitionv1alpha1.DefinitionGroupVersionKind),
		reconciler.WithExternalConnecter(&connector{
			kube:     mgr.GetClient(),
			log:      log,
			recorder: recorder,
		}),
		reconciler.WithLogger(log),
		reconciler.WithRecorder(event.NewAPIRecorder(recorder)))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&definitionv1alpha1.Definition{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

type connector struct {
	kube     client.Client
	log      logging.Logger
	recorder record.EventRecorder
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (reconciler.ExternalClient, error) {
	cr, ok := mg.(*definitionv1alpha1.Definition)
	if !ok {
		return nil, errors.New(errNotDefinition)
	}
	contents, _ := os.ReadFile(cr.Spec.SwaggerPath)
	d, err := libopenapi.NewDocument(contents)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	doc, modelErrors := d.BuildV3Model()
	if len(modelErrors) > 0 {
		return nil, fmt.Errorf("failed to build model: %w", errors.Join(modelErrors...))
	}
	if doc == nil {
		return nil, fmt.Errorf("failed to build model")
	}

	// Resolve model references
	resolvingErrors := doc.Index.GetResolver().Resolve()
	errs := []error{}
	for i := range resolvingErrors {
		c.log.Debug("Resolving error", "error", resolvingErrors[i].Error())
		errs = append(errs, resolvingErrors[i].ErrorRef)
	}
	if len(resolvingErrors) > 0 {
		return nil, fmt.Errorf("failed to resolve model references: %w", errors.Join(errs...))
	}

	return &external{
		kube: c.kube,
		log:  c.log,
		doc:  doc,
		rec:  c.recorder,
	}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	kube client.Client
	log  logging.Logger
	doc  *libopenapi.DocumentModel[v3.Document]
	rec  record.EventRecorder
}

func (e *external) Observe(ctx context.Context, mg resource.Managed) (reconciler.ExternalObservation, error) {
	cr, ok := mg.(*definitionv1alpha1.Definition)
	if !ok {
		return reconciler.ExternalObservation{}, errors.New(errNotDefinition)
	}

	if cr.Status.Created {
		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}

	return reconciler.ExternalObservation{
		ResourceExists: false,
	}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*definitionv1alpha1.Definition)
	if !ok {
		return errors.New(errNotDefinition)
	}

	crdsByte, err := e.GenerateCRDS(cr)
	if err != nil {
		return err
	}

	for _, crdB := range crdsByte {
		crd, err := crds.UnmarshalCRD(crdB)
		if err != nil {
			return err
		}
		if err := crds.InstallCRD(ctx, e.kube, crd); err != nil {
			return err
		}
		if cr.Labels == nil {
			cr.Labels = make(map[string]string)
		}

		dirty := false
		if _, ok := cr.Labels[labelKeyGroup]; !ok {
			dirty = true
			cr.Labels[labelKeyGroup] = cr.Spec.ResourceGroup
		}

		if dirty {
			err := e.kube.Update(ctx, cr, &client.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	cr.Status.Created = true
	e.kube.Status().Update(ctx, cr)

	e.log.Debug("Creating Definition", "Path:", cr.Spec.SwaggerPath, "Group:", cr.Spec.ResourceGroup)
	e.rec.Eventf(cr, corev1.EventTypeNormal, "DefinitionCreating",
		"Definition '%s/%s' creating", cr.Spec.SwaggerPath, cr.Spec.ResourceGroup)
	return nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) error {
	return nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	return nil
}

func (e *external) GenerateCRDS(cr *definitionv1alpha1.Definition) (map[string][]byte, error) {
	cfg := code.Options{
		Module:  "github.com/matteogastaldello/swaggergen-provider",
		Workdir: "./tmp/gen-crds/",
	}
	// clean := len(os.Getenv("GEN_CLEAN_WORKDIR")) == 0
	// if clean {
	// 	defer os.RemoveAll(cfg.Workdir)
	// }
	resources := cr.Spec.Resources
	byteSchema := make(map[string][]byte)
	var err error

	for _, resource := range resources {
		for _, verb := range resource.VerbsDescription {
			if strings.EqualFold(verb.Action, "create") && strings.EqualFold(verb.Method, "post") {
				path := e.doc.Model.Paths.PathItems.Value(verb.Path)
				if path == nil {
					return nil, fmt.Errorf("path %s not found", verb.Path)
				}
				bodySchema := path.Post.RequestBody.Content.Value("application/json").Schema //path.Post.RequestBody.Value.Content.Get("application/json").Schema
				if bodySchema == nil {
					return nil, fmt.Errorf("body schema not found for %s", verb.Path)
				}
				schema, err := bodySchema.BuildSchema()

				for _, param := range path.Post.Parameters {
					schema.Properties.Set(param.Name, param.Schema)
				}

				byteSchema[resource.Kind], err = generation.GenerateJsonSchemaFromSchemaProxy(base.CreateSchemaProxy(schema))
				if err != nil {
					return nil, err
				}
			}

		}
	}

	authSchemas := make(map[string][]byte)
	for secSchema := e.doc.Model.Components.SecuritySchemes.First(); secSchema != nil; secSchema = secSchema.Next() {
		byteSchema, err := generation.GenerateAuthSchemaFromSecuritySchema(secSchema.Value())
		if err != nil && err.Error() == generation.ErrInvalidSecuritySchema {
			e.log.Debug("Skipping invalid security schema", "type", secSchema.Value().Type, "scheme", secSchema.Value().Scheme)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("generating bytes auth schema %s: %w", secSchema.Key(), err)
		}
		authSchemas[secSchema.Key()] = byteSchema
	}
	for _, resource := range resources {
		err = code.Do(&code.Resource{
			Group:       cr.Spec.ResourceGroup,
			Version:     "v1alpha1",
			Kind:        text.CapitaliseFirstLetter(resource.Kind),
			Categories:  []string{strings.ToLower(resource.Kind)},
			Schema:      byteSchema[resource.Kind],
			Identifier:  resource.Identifier,
			AuthSchemas: &authSchemas,
			IsManaged:   true,
		},
			cfg,
		)
		if err != nil {
			return nil, fmt.Errorf("generating resource schema %s: %w", resource.Kind, err)
		}
	}

	for key, value := range authSchemas {
		err = code.Do(&code.Resource{
			Group:      cr.Spec.ResourceGroup,
			Version:    "v1alpha1",
			Kind:       fmt.Sprintf("%sAuth", text.CapitaliseFirstLetter(key)),
			Categories: []string{},
			Schema:     value,
			IsManaged:  false,
		},
			cfg,
		)
		if err != nil {
			return nil, fmt.Errorf("generating auth schema %s: %w", key, err)
		}
	}

	cmd := exec.Command("go", "mod", "init", cfg.Module)
	cmd.Dir = cfg.Workdir
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Performing 'go mod init' (workdir: %s, module: %s): %s", cfg.Workdir, cfg.Module, err)
	}

	cmd = exec.Command("go", "mod", "tidy")
	cmd.Dir = cfg.Workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return nil, fmt.Errorf("Performing 'go mod tidy' (workdir: %s, module: %s): %s", cfg.Workdir, cfg.Module, err)
		}
		return nil, fmt.Errorf("Performing 'go mod tidy' (workdir: %s, module: %s): %s", cfg.Workdir, cfg.Module, err)
	}
	cmd = exec.Command("go",
		"run",
		"--tags",
		"generate",
		"sigs.k8s.io/controller-tools/cmd/controller-gen",
		"object:headerFile=./hack/boilerplate.go.txt",
		"paths=./...", "crd:crdVersions=v1",
		"output:artifacts:config=./crds",
	)
	cmd.Dir = cfg.Workdir
	out, err = cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return nil, fmt.Errorf("Performing 'go run --tags generate...' (workdir: %s, module: %s): %s", cfg.Workdir, cfg.Module, err)
		}
		return nil, fmt.Errorf("Performing 'go run --tags generate...' (workdir: %s, module: %s): %s", cfg.Workdir, cfg.Module, err)
	}

	fsys := os.DirFS(cfg.Workdir)
	all, err := fs.ReadDir(fsys, "crds")
	if err != nil {
		return nil, fmt.Errorf("reading dir %s: %w", "crds", err)
	}

	crdsByte := make(map[string][]byte)
	for _, file := range all {
		fp, err := fsys.Open(filepath.Join("crds", file.Name()))
		if err != nil {
			return nil, fmt.Errorf("opening file %s: %w", file.Name(), err)
		}

		crdsByte[file.Name()], err = io.ReadAll(fp)
		if err != nil {
			return nil, fmt.Errorf("reading file %s: %w", file.Name(), err)
		}
		fp.Close()
	}

	return crdsByte, nil
}
