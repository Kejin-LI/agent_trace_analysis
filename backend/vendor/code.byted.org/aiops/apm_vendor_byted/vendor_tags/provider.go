package vendor_tags

import (
	"code.byted.org/gopkg/apm_vendor_interface"
)

const (
	kOsPPEFile         = "/opt/tmp/byteos/ppe"
	kTceClusterFile    = "/opt/tmp/tce/cluster"
	kTceSubClusterFile = "/opt/tmp/tce/subcluster"
)

type config struct {
	withDcHost bool
}

type Option func(conf *config)

// WithDcHost if add the dc、host tags
func WithDcHost(add bool) Option {
	return func(conf *config) {
		conf.withDcHost = add
	}
}

type bytedVendorTagsProvider struct {
	conf           config
	immutableTags  map[string]string
	rewritableTags map[string]string
}

func NewBytedVendorTagsProvider(ops ...Option) apm_vendor_interface.VendorTagsProvider {
	provider := &bytedVendorTagsProvider{conf: config{withDcHost: true}}
	for _, op := range ops {
		op(&provider.conf)
	}
	provider.init()
	return provider
}

func (b *bytedVendorTagsProvider) GetImmutableTags() map[string]string {
	return b.immutableTags
}

func (b *bytedVendorTagsProvider) GetRewritableTags() map[string]string {
	return b.rewritableTags
}

func (b *bytedVendorTagsProvider) GetTags() map[string]string {
	tags := make(map[string]string, len(b.immutableTags)+len(b.rewritableTags))
	for k, v := range b.rewritableTags {
		tags[k] = v
	}
	for k, v := range b.immutableTags {
		tags[k] = v
	}
	return tags
}

func (b *bytedVendorTagsProvider) init() {
	b.immutableTags, b.rewritableTags = getDefaultTags(b.conf.withDcHost)
}

type customVendorTagsProvider struct {
	immutableTags  map[string]string
	rewritableTags map[string]string
}

func (c *customVendorTagsProvider) GetImmutableTags() map[string]string {
	return c.immutableTags
}

func (c *customVendorTagsProvider) GetRewritableTags() map[string]string {
	return c.rewritableTags
}

func (c *customVendorTagsProvider) GetTags() map[string]string {
	tags := make(map[string]string, len(c.immutableTags)+len(c.rewritableTags))
	for k, v := range c.rewritableTags {
		tags[k] = v
	}
	for k, v := range c.immutableTags {
		tags[k] = v
	}
	return tags
}
