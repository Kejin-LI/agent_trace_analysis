package vendor_tags

import (
	"fmt"

	"code.byted.org/gopkg/apm_vendor_interface"
	"code.byted.org/gopkg/env"
)

const (
	TagPSM                = "_psm"
	TagPodIp              = "_pod_ip"
	TagPodName            = "_pod_name"
	TagCluster            = "_cluster"
	TagTcePhysicalCluster = "_tce_physical_cluster"
	TagDeployStage        = "_deploy_stage"
	TagIpv4               = "_ipv4"
	TagIpv6               = "_ipv6"
	TagEnv                = "_env"
	TagEnvType            = "_env_type"
	TagImageVersion       = "_image_version"
	TagIsSidecar          = "_is_sidecar"
	TagPrimaryPSM         = "_primary_psm"
	TagDC                 = "dc"
	TagHost               = "host"
)

const (
	useDefaultValueOnly tagBehavior = iota
	useInputValue
	useInputValueAsFallback
)

// Builder is used to build custom VendorTagsProvider,
// the builder contains some candidate tagKVs that
// can be used as default tag value
type Builder struct {
	tagsOpts map[string]tagConfig
}

type TagOption func(config *tagConfig)

// SetValue sets fixed value for the tag key
func SetValue(value string) TagOption {
	return func(config *tagConfig) {
		config.value = value
		config.behavior = useInputValue
	}
}

// SetFallbackValue sets a fallback value for the tag key,
// the fallback value will be used when the tag key not exists
// in the default candidate tags
func SetFallbackValue(value string) TagOption {
	return func(config *tagConfig) {
		config.value = value
		config.behavior = useInputValueAsFallback
	}
}

// SetRewritable mark the tag key as rewritable,
// rewritable means the tag could be overwritten by user-defined global tags
func SetRewritable() TagOption {
	return func(config *tagConfig) {
		config.rewritable = true
	}
}

type tagBehavior int8

type tagConfig struct {
	key        string
	value      string
	behavior   tagBehavior
	rewritable bool
}

func NewBuilder() *Builder {
	return &Builder{}
}

// AddTag add new or override exists tag with config options
// tag will be marked as immutable by default, can be modified using the SetRewritable option
// tag will use default value sdk provided, can be modified using the SetValue or SetFallbackValue option
func (b *Builder) AddTag(key string, ops ...TagOption) *Builder {
	if b.tagsOpts == nil {
		b.tagsOpts = map[string]tagConfig{}
	}
	cfg := &tagConfig{key: key, behavior: useDefaultValueOnly}
	for _, op := range ops {
		op(cfg)
	}
	b.tagsOpts[key] = *cfg
	return b
}

// AddAllDefaultTags enable all default tags sdk provided with default behavior(immutable or rewritable)
func (b *Builder) AddAllDefaultTags() *Builder {
	immutable, rewritable := getDefaultTags(true)
	for k, _ := range immutable {
		b.AddTag(k)
	}
	for k, _ := range rewritable {
		b.AddTag(k, SetRewritable())
	}
	return b
}

func (b *Builder) Build() (apm_vendor_interface.VendorTagsProvider, error) {
	immutable, rewritable := getDefaultTags(true)
	candidates := map[string]string{}
	for k, v := range rewritable {
		candidates[k] = v
	}
	for k, v := range immutable {
		candidates[k] = v
	}

	immutableTags := map[string]string{}
	rewritableTags := map[string]string{}

	for key, option := range b.tagsOpts {
		tags := immutableTags
		if option.rewritable {
			tags = rewritableTags
		}

		switch option.behavior {
		case useDefaultValueOnly:
			if value, ok := candidates[key]; ok {
				tags[key] = value
			}
		case useInputValue:
			tags[key] = option.value
		case useInputValueAsFallback:
			if value, ok := candidates[key]; ok {
				tags[key] = value
				continue
			}
			tags[key] = option.value
		default:
			return nil, fmt.Errorf("unsupported tag behavior: %v", option.behavior)
		}
	}

	// check must-contain tags
	for _, key := range getMustContainTags() {
		if _, ok := immutableTags[key]; ok {
			continue
		}
		if _, ok := rewritableTags[key]; ok {
			continue
		}
		return nil, fmt.Errorf("must define tag: %s", key)
	}

	return &customVendorTagsProvider{immutableTags: immutableTags, rewritableTags: rewritableTags}, nil
}

func getMustContainTags() []string {
	if env.InTCE() {
		return []string{TagDC, TagHost}
	} else if inByteOS() {
		return []string{TagDC, TagHost}
	} else {
		return []string{}
	}
}

func getDefaultTags(withDcHost bool) (immutableTags, rewritableTags map[string]string) {
	if env.InTCE() {
		immutableTags, rewritableTags = getTceVendorTags()
	} else if inByteOS() {
		immutableTags, rewritableTags = getByteOSVendorTags()
	} else {
		immutableTags = map[string]string{}
		rewritableTags = map[string]string{}
	}

	if withDcHost {
		immutableTags[TagDC] = GetDC()
		immutableTags[TagHost] = GetHost()
	}
	return
}

// setTceVendorTags sets the necessary tags for psm in tce environment
func getTceVendorTags() (immutableTags map[string]string, rewritableTags map[string]string) {
	immutableTags = map[string]string{
		TagPodIp:              GetPodIP(),
		TagPodName:            env.PodName(),
		TagCluster:            env.Cluster(),
		TagTcePhysicalCluster: GetPhysicalCluster(),
		TagDeployStage:        env.Stage(),
		TagEnv:                env.Env(),
		TagEnvType:            getHostEnv(),
		TagImageVersion:       env.ImageVersion(),
		TagIsSidecar:          getIsSidecar(),
	}
	rewritableTags = map[string]string{
		TagPSM:        env.PSM(),
		TagIpv4:       getIPV4(),
		TagIpv6:       getIPv6(),
		TagPrimaryPSM: getPrimaryPSM(),
	}
	return
}

func getByteOSVendorTags() (immutableTags map[string]string, rewritableTags map[string]string) {
	immutableTags = map[string]string{
		TagCluster: getByteOSCluster(),
		TagIpv4:    getByteOSIP(),
		TagIpv6:    getByteOSIPv6(),
	}
	rewritableTags = map[string]string{
		TagPSM: getByteOSPsm(),
	}
	return
}
