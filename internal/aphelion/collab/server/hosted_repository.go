package server

import "sdmm/internal/aphelion/collab/protocol"

// recoveredRepository returns a stored descriptor only when it is valid; an
// invalid or absent stored value is unknown (nil).
func recoveredRepository(stored *protocol.RepositoryDescriptor) *protocol.RepositoryDescriptor {
	if stored == nil || stored.Validate() != nil {
		return nil
	}
	copy := *stored
	return &copy
}

// hostedRepository returns the host-published repository descriptor, or nil
// (unknown). It is persisted with the hosted session and restored on recovery.
func (service *Service) hostedRepository(id string, include bool) *protocol.RepositoryDescriptor {
	if !include {
		return nil
	}
	service.mutex.RLock()
	defer service.mutex.RUnlock()
	return service.sessions[id].repository
}
