package v1alpha1

import (
	rtv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VerbsDescription struct {
	// Name of the action to perform when this api is called [create, update, list, get, delete]
	// +kubebuilder:validation:Enum=create;update;list;get;delete;
	// +immutable
	Action string `json:"action"`
	// Method: the http method to use [GET, POST, PUT, DELETE, PATCH]
	// +kubebuilder:validation:Enum=GET;POST;PUT;DELETE;PATCH
	// +immutable
	Method string `json:"method"`
	// Path: the path to the api - has to be the same path as the one in the swagger file you are referencing
	// +immutable
	Path string `json:"path"`
}

type Resources struct {
	// Name: the name of the resource to manage
	// +immutable
	Kind string `json:"kind"`
	// Identifier: the identifier of the resource to manage
	// +immutable
	Identifier string `json:"identifier"`
	// VerbsDescription: the list of verbs to use on this resource
	// +optional
	VerbsDescription []VerbsDescription `json:"verbsDescription"`
}

// DefinitionSpec is the specification of a Definition.
type DefinitionSpec struct {
	rtv1.ManagedSpec `json:",inline"`
	// Represent the path to the swagger file
	SwaggerPath string `json:"swaggerPath"`
	// Group: the group of the resource to manage
	// +immutable
	ResourceGroup string `json:"resourceGroup"`
	// The list of the resources to Manage
	// +optional
	Resources []Resources `json:"resources"`
}

// DefinitionStatus is the status of a Definition.
type DefinitionStatus struct {
	rtv1.ManagedStatus `json:",inline"`

	Created bool `json:"created"`
	// // Resource: the generated custom resource
	// // +optional
	// Resources  `json:"resource,omitempty"`

	// // PackageURL: .tgz or oci chart direct url
	// // +optional
	// PackageURL string `json:"packageUrl,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced,categories={krateo,definition,core}
//+kubebuilder:printcolumn:name="RESOURCE",type="string",JSONPath=".status.resource"
//+kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
//+kubebuilder:printcolumn:name="PACKAGE URL",type="string",JSONPath=".status.packageUrl"
//+kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp",priority=10

// Definition is a definition type with a spec and a status.
type Definition struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DefinitionSpec   `json:"spec,omitempty"`
	Status DefinitionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// DefinitionList is a list of Definition objects.
type DefinitionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Definition `json:"items"`
}
