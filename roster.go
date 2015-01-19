package murcott

import (
	"sync"

	"github.com/h2so5/murcott/utils"
)

// Roster represents a contact list.
type Roster struct {
	M     map[utils.NodeID]UserProfile
	mutex sync.RWMutex
}

func (r *Roster) Set(id utils.NodeID, prof UserProfile) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.M == nil {
		r.M = make(map[utils.NodeID]UserProfile)
	}
	r.M[id] = prof
}

func (r *Roster) Get(id utils.NodeID) UserProfile {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.M[id]
}

func (r *Roster) List() []utils.NodeID {
	var l []utils.NodeID
	for n, _ := range r.M {
		l = append(l, n)
	}
	return l
}
