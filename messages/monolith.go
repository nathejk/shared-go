package messages

import "github.com/nathejk/shared-go/types"

// monolith:team_updated
type MonolithNathejkTeam struct {
	Entity struct {
		ID                   types.TeamID         `json:"id"`
		RemoteID             string               `json:"remoteId"`
		CreatedUts           types.UnixtimeString `json"createdUts"`
		VerifiedUts          types.UnixtimeString `json"verifiedUts"`
		OpenedUts            types.UnixtimeString `json"openedUts"`
		FinishedUts          types.UnixtimeString `json"finishedUts"`
		CanceledUts          types.UnixtimeString `json"canceledUts"`
		DeletedUts           types.UnixtimeString `json"deletedUts"`
		LastModifyUts        types.UnixtimeString `json"lastModifyUts"`
		StartUts             types.UnixtimeString `json"startUts"`
		FinishUts            types.UnixtimeString `json"finishUts"`
		SignupStatusTypeName string               `json"signupStatusTypeName"`
		TypeName             types.TeamType       `json"typeName"`
		ParentTeamID         types.TeamID         `json"parentTeamId"`
		IP                   string               `json"ip"`
		Title                string               `json"title"`
		Gruppe               string               `json"gruppe"`
		Division             string               `json"division"`
		Korps                string               `json"korps"`
		ContactTitle         string               `json"contactTitle"`
		ContactAddress       string               `json"contactAddress"`
		ContactPostalCode    string               `json"contactPostalCode"`
		ContactMail          types.EmailAddress   `json"contactMail"`
		ContactPhone         types.PhoneNumber    `json"contactPhone"`
		ContactRole          string               `json"contactRole"`
		TeamNumber           string               `json"teamNumber"`
		LigaNumber           string               `json"ligaNumber"`
		LigaNumberVerified   *string              `json"ligaNumberVerified"`
		LokNumber            string               `json"lokNumber"`
		MemberCount          string               `json"memberCount"`
		PaidMemberCount      string               `json"paidMemberCount"`
		Paid                 string               `json"paid"`
		PaidStatus           *string              `json"paidStatus"`
		Remark               string               `json"remark"`
		PhotoID              string               `json"photoId"`
		PhotoUts             types.UnixtimeString `json"photoUts"`
		AdvSpejdNightCount   string               `json"advSpejdNightCount"`
		ArrivalName          string               `json"arrivalName"`
		CheckedAtStart       string               `json"checkedAtStart"`
	} `json:"entity"`
	Changed []string `json:"changed"`
	Sql     struct {
		Sql string `json:"nathejk_team"`
	} `json:"sql"`
}

// monolith:nathejk_payment
type MonolithNathejkPayment struct {
	Entity struct {
		ID      string               `json:"id"`
		OrderID string               `json:"orderId"`
		TeamID  types.TeamID         `json:"teamId"`
		Uts     types.UnixtimeString `json;"uts"`
		Amount  string               `json:"amount"`
	} `json:"entity"`
	Changed []string `json:"changed"`
	Sql     struct {
		Sql string `json:"nathejk_payment"`
	} `json:"sql"`
}

// monolith:nathejk_member
type MonolithNathejkMember struct {
	Entity struct {
		ID              types.MemberID       `json:"id"`
		TeamID          types.TeamID         `json:"teamId"`
		Number          string               `json;"number"`
		CreatedUts      types.UnixtimeString `json:"createdUts"`
		PausedUts       types.UnixtimeString `json:"pausedUts"`
		InhqUts         types.UnixtimeString `json:"inhqUts"`
		DiscontinuedUts types.UnixtimeString `json:"discontinuedUts"`
		DeletedUts      types.UnixtimeString `json:"deletedUts"`
		Title           string               `json:"title"`
		Address         string               `json:"address"`
		PostalCode      string               `json:"postalCode"`
		Mail            types.EmailAddress   `json:"mail"`
		Phone           types.PhoneNumber    `json:"phone"`
		ContactPhone    types.PhoneNumber    `json:"contactPhone"`
		ContactSms      string               `json:"contactSms"`
		SpejderTelefon  string               `json:"spejderTelefon"`
		BirthDate       string               `json:"birthDate"`
		Returning       string               `json:"returning"`
		Remark          string               `json:"remark"`
		PhotoID         string               `json:"photoId"`
	} `json:"entity"`
	Changed []string `json:"changed"`
	Sql     struct {
		Sql string `json:"nathejk_payment"`
	} `json:"sql"`
}
