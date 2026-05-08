package registry

type Named interface {
	Name() string
}

type OrderedRegistry[T Named] struct {
	items map[string]T
	order []string
}

func New[T Named]() *OrderedRegistry[T] {
	return &OrderedRegistry[T]{items: make(map[string]T)}
}

func (r *OrderedRegistry[T]) Register(item T) {
	name := item.Name()
	if _, exists := r.items[name]; !exists {
		r.order = append(r.order, name)
	}
	r.items[name] = item
}

func (r *OrderedRegistry[T]) Get(name string) (T, bool) {
	item, ok := r.items[name]
	return item, ok
}

func (r *OrderedRegistry[T]) Has(name string) bool {
	_, ok := r.items[name]
	return ok
}

func (r *OrderedRegistry[T]) All() []T {
	result := make([]T, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.items[name])
	}
	return result
}

func (r *OrderedRegistry[T]) Names() []string {
	return append([]string(nil), r.order...)
}
