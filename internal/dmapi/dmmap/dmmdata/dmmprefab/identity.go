// APHELION EDIT ADDITION START - CONTENT IDENTITY
package dmmprefab

import "sdmm/internal/aphelion/prefabidentity"

// Equals compares saved content, never the local cache/UI identifier.
func (p *Prefab) Equals(other *Prefab) bool {
	if p == nil || other == nil {
		return p == other
	}
	return prefabidentity.Equal(p.path, p.vars, other.path, other.vars)
}

// APHELION EDIT ADDITION END
