package messages

import "github.com/nathejk/shared-go/types"

// nathejk:qr.found
type NathejkQrFound struct {
	QrID         types.QrID `json:"qrId"`
	ScannerID    string     `json:"scannerId"`
	ScannerPhone string     `json:"scannerPhone"`
}

// nathejk:qr.registered
type NathejkQrRegistered struct {
	QrID         types.QrID        `json:"qrId"`
	TeamID       types.TeamID      `json:"teamId"`
	TeamNumber   string            `json:"teamNumber"`
	ScannerID    string            `json:"scannerId"`
	ScannerPhone types.PhoneNumber `json:"scannerPhone"`
}

// nathejk:qr.scanned
type NathejkQrScanned struct {
	QrID         types.QrID        `json:"qrId"`
	TeamID       types.TeamID      `json:"teamId"`
	TeamNumber   string            `json:"teamNumber"`
	ScannerID    string            `json:"scannerId"`
	ScannerPhone types.PhoneNumber `json:"scannerPhone"`
	Remark       string            `json:"remark"`
	Location     struct {
		Latitude  string `json:"lat"`
		Longitude string `json:"lon"`
	} `json:"location"`
}
