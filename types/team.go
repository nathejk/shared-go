package types

import "github.com/google/uuid"

// TeamID
type TeamID ID

func (t TeamID) New() TeamID {
	return TeamID("team-" + uuid.New().String())
}

// TeamIDs
type TeamIDs []TeamID

func (i TeamIDs) Add(ID TeamID) TeamIDs {
	return append(i, ID)
}
func (i TeamIDs) AddUnique(ID TeamID) TeamIDs {
	if i.Exists(ID) {
		return i
	}
	return i.Add(ID)
}
func (IDs TeamIDs) Exists(key TeamID) bool {
	for _, prop := range IDs {
		if prop == key {
			return true
		}
	}
	return false
}
func (ids TeamIDs) Diff(slices ...TeamIDs) TeamIDs {
	m := map[TeamID]bool{}
	for _, id := range ids {
		m[id] = true
	}
	for _, slice := range slices {
		for _, id := range slice {
			if _, found := m[id]; found {
				delete(m, id)
			}
		}
	}
	var diff TeamIDs
	for _, id := range ids {
		if _, found := m[id]; found {
			diff = diff.Add(id)
		}
	}
	return diff
}
