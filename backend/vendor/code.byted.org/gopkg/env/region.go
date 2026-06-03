package env

import (
	"encoding/json"
	"io/ioutil"
	"sync/atomic"
	"time"
)

const (
	// UnknownRegion .
	UnknownRegion      = "-"
	R_CN               = "CN"
	R_SG               = "SG"
	R_US               = "US"
	R_MALIVA           = "MALIVA"
	R_ALISG            = "ALISG" // Singapore Aliyun
	R_CA               = "CA"    // West America
	R_BOE              = "BOE"
	R_SUITEKA          = "SUITEKA"
	R_SUITEKA2         = "SUITEKA2"
	R_SUITEKA3         = "SUITEKA3"
	R_YCRU             = "YCRU"
	R_I18N             = "I18N"
	R_USTTP            = "US-TTP"
	R_CNTOB            = "CN-TOB"
	R_USEast           = "US-East"
	R_USBOE            = "US-BOE" // BOETTP
	R_SingaporeCentral = "Singapore-Central"
	R_AsiaSouth        = "Asia-South"
	R_AsiaSaaS         = "Asia-SaaS"
	R_EuropeCentral    = "Europe-Central"
	R_QMSG             = "QMSG"
	R_USTTP2           = "US-TTP2"
	R_USCentral        = "US-Central"
	R_EUTTP            = "EU-TTP"
	R_ChinaPay         = "China-Pay"
	R_ChinaVolcEast    = "ChinaVolc-East"
	R_ChinaVolcSouth   = "ChinaVolc-South"
	R_ChinaEast        = "China-East"
	R_EUCOMPLIANCE2    = "EU-Compliance2"
	R_EUCOMPLIANCE     = "EU-Compliance"
)

const regionFileDefault = "/opt/tiger/chadc/region.json"
const regionRefreshDur = 5 * time.Minute

var (
	region     atomic.Value // local region (string)
	regionFile atomic.Value // file name (string)
	idcRegion  atomic.Value // key is IDC, value is region (map[string]string)
)

var dcToRegionMap = map[string]string{
	DC_HY:     R_CN,
	DC_LF:     R_CN,
	DC_HL:     R_CN,
	DC_YG:     R_CN,
	DC_WJ:     R_CN,
	DC_SDQD:   R_CN,
	DC_SDQDCJ: R_CN,
	DC_ALINC2: R_CN,
	DC_LQ:     R_CN,
	DC_AGSY:   R_CN,
	DC_AGCQ:   R_CN,
	DC_AGGDSZ: R_CN,
	DC_AGHBWH: R_CN,
	DC_AGJSNJ: R_CN,
	DC_AGLNSY: R_CN,
	DC_AGSDQD: R_CN,
	DC_AGSXXA: R_CN,
	DC_AGSXLQ: R_CN,

	DC_CA: R_CA,

	DC_MALIVA:  R_MALIVA,
	DC_USEAST3: R_MALIVA,
	DC_USEAST4: R_MALIVA,

	DC_USEAST2A: R_I18N,

	DC_SG: R_SG,

	DC_SGEA1:           R_QMSG,
	DC_SGEA2:           R_QMSG,
	DC_SGEE1:           R_QMSG,
	DC_SGEE2:           R_QMSG,
	DC_SGSAAS1LARKIDC1: R_QMSG,
	DC_SGSAAS1LARKIDC2: R_QMSG,

	DC_BOE:  R_BOE,
	DC_IBOE: R_BOE,
	DC_COF:  R_BOE,

	DC_BOETTP: R_USBOE,

	DC_VA: R_US,

	DC_ALISG:        R_ALISG,
	DC_SG1:          R_ALISG,
	DC_SGCOMM1:      R_ALISG,
	DC_SGCOMPLIANCE: R_ALISG,
	DC_MY:           R_ALISG,

	DC_SYKALARK: R_SUITEKA,

	DC_SYKA2LARK: R_SUITEKA2,

	DC_SYKA3LARK: R_SUITEKA3,

	DC_YCRU: R_YCRU,

	DC_USEAST5: R_USTTP,

	DC_USEAST8: R_USTTP2,

	DC_USORDX: R_USCentral,

	DC_LFTOBIAAS: R_CNTOB,
	DC_HLTOBIAAS: R_CNTOB,
	DC_NTTOBIAAS: R_CNTOB,
	DC_GZTOBIAAS: R_CNTOB,

	DC_USEASTDT:  R_USEast,
	DC_USTSENTRY: R_USEast,

	DC_SGDT: R_SingaporeCentral,
	DC_SG2:  R_SingaporeCentral,

	DC_IND:   R_AsiaSouth,
	DC_INDDP: R_AsiaSouth,

	DC_JPSAAS: R_AsiaSaaS,

	DC_VOLCNT: R_ChinaVolcEast,

	DC_VOLCGZ: R_ChinaVolcSouth,

	DC_AWSFR:         R_EuropeCentral,
	DC_FRACOMPLIANCE: R_EuropeCentral,

	DC_IE: R_EUTTP,

	DC_LFZG: R_ChinaPay,
	DC_HLZG: R_ChinaPay,

	DC_IE2: R_EUCOMPLIANCE2,
	DC_DE:  R_EUCOMPLIANCE,

	DC_PD: R_ChinaEast,
	DC_HJ: R_ChinaEast,
}

// Region .
func Region() string {
	if v := region.Load(); v != nil {
		return v.(string)
	}

	idc := IDC()
	regionResult := GetRegionFromIDC(idc)
	region.Store(regionResult)
	return regionResult
}

// GetRegionFromIDC .
func GetRegionFromIDC(idc string) string {
	if r, ok := dcToRegionMap[idc]; ok {
		return r
	}

	if m, ok := idcRegion.Load().(map[string]string); ok {
		if r, ok := m[idc]; ok {
			return r
		}
	}
	return UnknownRegion
}

// SetRegionFile .
func SetRegionFile(file string) {
	regionFile.Store(file)
	updateRegionIDCs()
}

// hasIDC return true if idc is support
func hasIDC(idc string) bool {
	m, _ := idcRegion.Load().(map[string]string)
	_, ok := m[idc]
	return ok
}

// idcList return IDCs which supported
func idcList() []string {
	m, _ := idcRegion.Load().(map[string]string)
	idcList := make([]string, 0, len(m))
	for dc := range m {
		idcList = append(idcList, dc)
	}
	return idcList
}

func init() {
	regionFile.Store(regionFileDefault)
	refreshRegion()
}

// ATTENTION: IT COMES WITH A LOOP, DON'T CALL IT AGAIN.
func refreshRegion() {
	defer time.AfterFunc(regionRefreshDur, refreshRegion)
	updateRegionIDCs()
}

func updateRegionIDCs() {
	// newRegionIDCs, key is region, value is []IDC
	newRegionIDCs := make(map[string][]string)
	file, _ := regionFile.Load().(string)
	content, err := ioutil.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(content, &newRegionIDCs)
	}
	// if error, skip update
	if err != nil {
		return
	}
	newIDCRegion := make(map[string]string)
	for region, dcs := range newRegionIDCs {
		for _, dc := range dcs {
			newIDCRegion[dc] = region
		}
	}
	idcRegion.Store(newIDCRegion)
}
