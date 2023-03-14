package messages

import (
	"net/http"
	"strings"

	"github.com/nathejk/shared-go/types"
)

type MetaID struct {
	ID string `json:"id"`
}

type ByUserMeta struct {
	UserID types.UserID `json:"userId"`
}

type MetadataRequestHeaders map[string]string

func (h *MetadataRequestHeaders) Set(header http.Header) {
	mrh := MetadataRequestHeaders{}
	for key, values := range header {
		mrh[key] = strings.Join(values, "\n")
	}
	*h = mrh
}

type Metadata struct {
	Producer            string                 `json:"producer"`
	Phase               string                 `json:"phase,omitempty"`
	RequestHeaders      MetadataRequestHeaders `json:"requestHeaders,omitempty"`
	CorrelationSequence uint64                 `json:"correlationSequence,omitempty"`
	LegacyMemberID      types.ID               `json:"legacyMemberId,omitempty"`
	LegacyTeamID        types.LegacyID         `json:"legacyTeamId,omitempty"`
}
