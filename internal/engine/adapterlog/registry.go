package adapterlog

type Registry struct {
	parsers map[string]Parser
}

func NewRegistry() *Registry {
	return &Registry{parsers: map[string]Parser{}}
}

func (r *Registry) Register(adapterKind string, parser Parser) {
	if r == nil || parser == nil {
		return
	}
	r.parsers[adapterKind] = parser
}

func (r *Registry) ParserFor(adapterKind string) Parser {
	if r == nil {
		return NoopParser{}
	}
	parser, ok := r.parsers[adapterKind]
	if !ok || parser == nil {
		return NoopParser{}
	}
	return parser
}
