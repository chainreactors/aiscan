package service

import (
	"context"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/pkg/web"
	managementapi "github.com/chainreactors/cyber/pkg/web/api"
)

// Capability is the server-side seam for an independently deployable Web
// feature. Core service code only knows the lifecycle and registration hooks;
// domain protobufs, storage models and protocol handlers stay in the feature.
type Capability interface {
	ID() string
	Manifest() web.Capability
	SchemaModules() []SchemaModule
	Routes(*managementapi.API) []web.Route
	RegisterNamespaces(*aop.NamespaceMux) error
	Close(context.Context) error
}
