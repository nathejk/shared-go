package types_test

import (
	"testing"

	"github.com/nathejk/shared-go/types"
	"github.com/stretchr/testify/assert"
)

func TestLegacyIDChecksum(t *testing.T) {
	assert := assert.New(t)

	assert.Equal("e3330", types.LegacyID("2022562").Checksum(), "Checksum calculation failed")
	assert.Equal(uint(2022), types.LegacyID("2022562").Year(), "Year calculation failed")
	/*
	   err = msg.SetMeta("WORLD")
	   assert.Nil(err)
	   assert.Equal("\"WORLD\"", string(msg.RawMeta().(json.RawMessage)))
	   assert.NotEqual("", msg.EventID(), "Event id should not be empty")
	*/
}

func TestMemberIDNewFunc(t *testing.T) {
	assert := assert.New(t)

	assert.True(types.Slug("test").Valid(), "Slug validation failed")
	assert.True(types.Slug("2026").Valid(), "Slug validation failed")
	assert.False(types.Slug("").Valid(), "Emoty slug not valid")
	assert.False(types.Slug("-2026").Valid(), "Slug validation failed")
	assert.False(types.Slug("hello world").Valid(), "Spaces not allowed")
	assert.False(types.Slug("æøå").Valid(), "Only latin1 characters allowed")

	//ID := memberID.New()
	//assert.Equal("  ", memberID, "Year calculation failed")
	//assert.Equal(ID, memberID, "Year calculation failed")
}
