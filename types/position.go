package types

type Latitude float64
type Longitude float64

type Position struct {
	Latitude  Latitude  `json:"latitude"`
	Longitude Longitude `json:"longitude"`
}
