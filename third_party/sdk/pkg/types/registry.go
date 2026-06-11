package types

// EngineFactory creates an engine from engine-specific config.
type EngineFactory func(config interface{}) (Engine, error)

// RegisterFunc receives engine registrations from engine packages.
type RegisterFunc func(name string, factory EngineFactory)

var globalRegister RegisterFunc

var pendingRegistrations []struct {
	name    string
	factory EngineFactory
}

// SetRegisterFunc sets the global engine registry hook.
func SetRegisterFunc(fn RegisterFunc) {
	globalRegister = fn
	for _, reg := range pendingRegistrations {
		globalRegister(reg.name, reg.factory)
	}
	pendingRegistrations = nil
}

// Register registers an engine factory. Engine packages call this from init.
func Register(name string, factory EngineFactory) {
	if globalRegister != nil {
		globalRegister(name, factory)
		return
	}
	pendingRegistrations = append(pendingRegistrations, struct {
		name    string
		factory EngineFactory
	}{name, factory})
}
