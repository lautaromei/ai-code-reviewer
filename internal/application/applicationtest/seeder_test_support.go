package applicationtest

type testSeeder[T any] struct {
	persist func(T) error
}

func newTestSeeder[T any](persist func(T) error) testSeeder[T] {
	if persist == nil {
		panic("applicationtest: nil persist function")
	}
	return testSeeder[T]{persist: persist}
}

func (s testSeeder[T]) One(value T) T {
	if err := s.persist(value); err != nil {
		panic(err)
	}
	return value
}

func (s testSeeder[T]) Many(values ...T) []T {
	for _, value := range values {
		s.One(value)
	}
	return values
}

func (s testSeeder[T]) Generate(count int, generate func() T) []T {
	if count < 0 {
		panic("applicationtest: negative seed count")
	}
	if generate == nil {
		panic("applicationtest: nil generator function")
	}
	values := make([]T, 0, count)
	for range count {
		values = append(values, s.One(generate()))
	}
	return values
}

func (s testSeeder[T]) Persist(builder interface{ Bare() T }) T {
	if builder == nil {
		panic("applicationtest: nil builder")
	}
	return s.One(builder.Bare())
}
