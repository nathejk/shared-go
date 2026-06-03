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

func (slug CorpsSlug) Label() string {
	return CorpsLabels[slug]
}

type SlugLabel struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

func (list CorpsSlugList) AsObjects() (sl []SlugLabel) {
	for _, slug := range list {
		sl = append(sl, SlugLabel{Slug: string(slug), Label: slug.Label()})
	}
	return sl
}
