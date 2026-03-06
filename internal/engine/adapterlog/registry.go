package adapterlog

type Registry struct {
	factories map[string]func() Parser
}

func NewRegistry() *Registry {
	return &Registry{factories: map[string]func() Parser{}}
}

func (r *Registry) Register(adapterKind string, parser Parser) {
	if r == nil || parser == nil {
		return
	}
	r.factories[adapterKind] = func() Parser {
		return parser
	}
}

func (r *Registry) RegisterFactory(adapterKind string, factory func() Parser) {
	if r == nil || factory == nil {
		return
	}
	r.factories[adapterKind] = factory
}

func (r *Registry) ParserFor(adapterKind string) Parser {
	if r == nil {
		return NoopParser{}
	}
	factory, ok := r.factories[adapterKind]
	if !ok || factory == nil {
		return NoopParser{}
	}
	parser := factory()
	if parser == nil {
		return NoopParser{}
	}
	return parser
}
