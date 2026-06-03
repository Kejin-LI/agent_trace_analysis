package apm_vendor_interface

type VendorTagsProvider interface {
	// GetImmutableTags return tags which should not be overwritten
	GetImmutableTags() map[string]string
	// GetRewritableTags return tags that can overwritten by global tags
	GetRewritableTags() map[string]string
	// GetTags return all tags
	GetTags() map[string]string
}
