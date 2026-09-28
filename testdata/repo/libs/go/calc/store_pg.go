package calc

// Store is the Postgres adapter fixture.
type Store struct{ rows []int }

// Load reads rows.
func (s *Store) Load() []int {
	if s == nil {
		return nil
	}
	return s.rows
}
