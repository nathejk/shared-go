package types

type MailTemplateData_Contact struct {
	Name  string      `json:"navn"`
	Phone PhoneNumber `json:"telefon"`
	Email Email       `json:"email"`
	Role  string      `json:"rolle"`
}
type MailTemplateData_Member struct {
	Name        string
	Address     string
	PostalCode  string
	City        string
	Email       Email
	Phone       PhoneNumber
	PhoneParent PhoneNumber
	Birthday    Date
	Returning   bool
}
type MailTemplateData struct {
	Name         string       `json:"hold"`
	Group        string       `json:"gruppe"`
	Corps        string       `json:"korps"`
	SignupStatus SignupStatus `json:"status"`

	Contact MailTemplateData_Contact  `json:"kontakt"`
	Members []MailTemplateData_Member `json:"deltagere"`

	Nathejk string `json:"nathejk"`
	Weekend string `json:"weekend"`
}
