package projects

import (
	"sort"
	"sync"
)

// Repository stores projects. The module binds it to MemoryRepository with
// gonest.Bind; tests can replace it with gonesttest.OverrideProvider.
type Repository interface {
	// List returns the tenant's projects ordered by ID.
	List(tenantID string) []Project
	Get(tenantID string, id int) (Project, bool)
	// Insert assigns an ID and stores p.
	Insert(p Project) Project
	// Update replaces an existing project; false if it does not exist.
	Update(p Project) bool
	// Delete removes a project; false if it does not exist.
	Delete(tenantID string, id int) bool
}

// MemoryRepository is a goroutine-safe in-memory Repository.
type MemoryRepository struct {
	mu     sync.RWMutex
	nextID int
	rows   map[int]Project
}

// NewMemoryRepository returns an empty repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{nextID: 1, rows: map[int]Project{}}
}

// List implements Repository.
func (r *MemoryRepository) List(tenantID string) []Project {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Project
	for _, p := range r.rows {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get implements Repository.
func (r *MemoryRepository) Get(tenantID string, id int) (Project, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.rows[id]
	if !ok || p.TenantID != tenantID {
		return Project{}, false
	}
	return p, true
}

// Insert implements Repository.
func (r *MemoryRepository) Insert(p Project) Project {
	r.mu.Lock()
	defer r.mu.Unlock()
	p.ID = r.nextID
	r.nextID++
	r.rows[p.ID] = p
	return p
}

// Update implements Repository.
func (r *MemoryRepository) Update(p Project) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.rows[p.ID]
	if !ok || old.TenantID != p.TenantID {
		return false
	}
	r.rows[p.ID] = p
	return true
}

// Delete implements Repository.
func (r *MemoryRepository) Delete(tenantID string, id int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.rows[id]
	if !ok || p.TenantID != tenantID {
		return false
	}
	delete(r.rows, id)
	return true
}
