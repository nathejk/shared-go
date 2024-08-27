package types

type CorpsSlug string
type CorpsSlugList []CorpsSlug

const (
	CorpsSlugDDS   CorpsSlug = "dds"
	CorpsSlugKFUM  CorpsSlug = "kfum"
	CorpsSlugKFUK  CorpsSlug = "kfuk"
	CorpsSlugDBS   CorpsSlug = "dbs"
	CorpsSlugDGS   CorpsSlug = "dgs"
	CorpsSlugDSS   CorpsSlug = "dss"
	CorpsSlugFDF   CorpsSlug = "fdf"
	CorpsSlugOther CorpsSlug = "andet"
)

var CorpsSlugs = CorpsSlugList{CorpsSlugDDS, CorpsSlugKFUM, CorpsSlugKFUK, CorpsSlugDBS, CorpsSlugDGS, CorpsSlugDSS, CorpsSlugFDF, CorpsSlugOther}
var CorpsLabels = map[CorpsSlug]string{
	CorpsSlugDDS:   "Det Danske Spejderkorps",
	CorpsSlugKFUM:  "KFUM-Spejderne",
	CorpsSlugKFUK:  "De grønne pigespejdere",
	CorpsSlugDBS:   "Danske Baptisters Spejderkorps",
	CorpsSlugDGS:   "De Gule Spejdere",
	CorpsSlugDSS:   "Dansk Spejderkorps Sydslesvig",
	CorpsSlugFDF:   "FDF / FPF",
	CorpsSlugOther: "Andet",
}

func (slug CorpsSlug) String() string {
	return CorpsLabels[slug]
}
