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

func UniqueTeamID(teamIDs []TeamID) []TeamID {
	keys := make(map[TeamID]bool)
	list := []TeamID{}
	for _, entry := range teamIDs {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func DiffTeamID(slice1 TeamIDs, slices ...TeamIDs) TeamIDs {
	diff := TeamIDs{}

	// Loop two times, first to find slice1 strings not in slice2,
	// second loop to find slice2 strings not in slice1
	//	for i := 0; i < 2; i++ {
	for _, id := range slice1 {
		found := false
		for _, slice := range slices {
			if slice.Exists(id) {
				found = true
				break
			}
		}
		// String not found. We add it to return slice
		if !found {
			diff = append(diff, id)
		}
	}

	return diff
}
