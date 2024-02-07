/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type GhostBlogConfigSpec struct {
	// Url defines the url that will be used
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Url string `json:"url,omitempty"`
}

type GhostDatabaseConnectionSpec struct {
	// sqlite filename.
	// +optional
	Filename string `json:"filename,omitempty"`
	// mysql host
	// +optional
	Host string `json:"host,omitempty"`
	// mysql port
	// +optional
	Port intstr.IntOrString `json:"port,omitempty"`
	// mysql database user
	// +optional
	User string `json:"user,omitempty"`
	// mysql database password of user
	// +optional
	Password string `json:"password,omitempty"`
	// mysql database name
	// +optional
	Database string `json:"database,omitempty"`
}

type GhostServerSpec struct {
	Host string             `json:"host"`
	Port intstr.IntOrString `json:"port"`
}

// GhostDatabaseSpec defines ghost database config.
// https://ghost.org/docs/concepts/config/#database
type GhostDatabaseSpec struct {
	// Client is ghost database client.
	// +kubebuilder:validation:Enum=sqlite3;mysql
	Client string `json:"client"`
	// +optional
	Connection GhostDatabaseConnectionSpec `json:"connection"`
}

type GhostConfigSpec struct {
	URL string `json:"url"`

	Database GhostDatabaseSpec `json:"database"`
	// +optional
	Server GhostServerSpec `json:"server"`
}

// GhostPersistentSpec defines peristent volume
type GhostPersistentSpec struct {
	Enabled bool `json:"enabled"`
	// If defined, will create persistentVolumeClaim with spesific storageClass name.
	// If undefined (the default) or set to null, no storageClassName spec is set, choosing the default provisioner.
	// +nullable
	StorageClass *string `json:"storageClass,omitempty"`
	// size of storage
	Size resource.Quantity `json:"size"`
}

// GhostIngressTLSSpec defines ingress tls
type GhostIngressTLSSpec struct {
	Enabled    bool   `json:"enabled"`
	SecretName string `json:"secretName"`
}

// GhostIngressSpec defines ingress
type GhostIngressSpec struct {
	Enabled bool `json:"enabled"`
	// +optional
	// +listType=set
	Hosts []string `json:"hosts,omitempty"`
	// +optional
	TLS GhostIngressTLSSpec `json:"tls,omitempty"`
	// Additional annotations passed to ".metadata.annotations" in networking.k8s.io/ingress object.
	// This is useful for configuring ingress through annotation field like: ingress-class, static-ip, etc
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// GhostBlogSpec defines the desired state of GhostBlog
type GhostBlogSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Size defines the number of GhostBlog instances
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Size int32 `json:"size,omitempty"`

	// Port defines the port that will be used to init the container with the image
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	ContainerPort int32 `json:"containerPort,omitempty"`

	// Image defines the image that will be used to init the container
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Image string `json:"image,omitempty"`

	// Ghost configuration. This field will be written as ghost configuration. Saved in configmap and mounted
	// in /etc/ghost/config/config.json and symlinked to /var/lib/ghost/config.production.json
	Config GhostConfigSpec `json:"config"`
	// +optional
	Persistent GhostPersistentSpec `json:"persistent,omitempty"`
	// +optional
	Ingress GhostIngressSpec `json:"ingress,omitempty"`
}

// GhostBlogStatus defines the observed state of GhostBlog
type GhostBlogStatus struct {
	// Represents the observations of a GhostBlog's current state.
	// GhostBlog.status.conditions.type are: "Available", "Progressing", and "Degraded"
	// GhostBlog.status.conditions.status are one of True, False, Unknown.
	// GhostBlog.status.conditions.reason the value should be a CamelCase string and producers of specific
	// condition types may define expected values and meanings for this field, and whether the values
	// are considered a guaranteed API.
	// GhostBlog.status.conditions.Message is a human readable message indicating details about the transition.
	// For further information see: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// Conditions store the status conditions of the GhostBlog instances
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GhostBlog is the Schema for the ghostblogs API
type GhostBlog struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GhostBlogSpec   `json:"spec,omitempty"`
	Status GhostBlogStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GhostBlogList contains a list of GhostBlog
type GhostBlogList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GhostBlog `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GhostBlog{}, &GhostBlogList{})
}
