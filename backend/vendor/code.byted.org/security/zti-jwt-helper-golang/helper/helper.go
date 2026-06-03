package helper

import (
	"bufio"
	"context"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/zeebo/errs"

	"code.byted.org/gopkg/env"
	"code.byted.org/security/go-spiffe-v2/spiffeid"
	"code.byted.org/security/go-spiffe-v2/svid/jwtsvid"
	"code.byted.org/security/go-spiffe-v2/workloadapi"

	"gopkg.in/square/go-jose.v2/json"
	"gopkg.in/square/go-jose.v2/jwt"
)

const (
	defaultAudience    = "zti"
	tokenPathEnv       = "SEC_TOKEN_PATH"
	tokenStrEnv        = "SEC_TOKEN_STRING"
	agentSocketPathEnv = "ZTI_AGENT_SOCKET_PATH"

	defaultContextTimeout           = time.Second * 5
	defaultUpdateIntervalSecs int64 = 180
	// daemon interval token refresh duration
	daemonUpdateInterval = time.Hour * 2
	// backward compatible with dps agent: need to deprecate in future version
	dpsAgentUserEnv    = "INFSEC_SEC_USER"
	dpsAgentPSMEnv     = "INFSEC_SEC_PSM"
	dpsAgentEnablePath = "GET_SEC_TOKEN_STRING_FROM_DAEMON"
	dpsAgentSocketPath = "/opt/tmp/sock/.unix_sock_agent_1234567890.sock"

	// Used to determine whether to fallback zti-agent
	fallbackZTIAgentEnv = "FALLBACK_ZTI_AGENT"

	tokenSourceString = "sec_token_string"
	tokenSourcePath   = "sec_token_path"
	tokenSourceDPS    = "dps_agent"
	tokenSourceZTI    = "zti_agent"
)

// backward compatible with dps agent: need to deprecate in future version
type CommandTag struct {
	Cmd  uint8  `json:"cmd"`
	PSM  string `json:"psm"`
	User string `json:"user"`
}

var (
	// global token string for all the token access function
	globalToken string

	tokenPath   string
	tokenStr    string
	tokenSource string

	customServiceIdentity string
	customTokenPath       string
	customTokenStr        string

	updateTime     int64
	workloadClient *workloadapi.Client
	rwLock         sync.RWMutex
	// Add routineMutex for protecting refreshSVID
	routineMutex sync.Mutex
	// grpc call timeout for zti-agent and dps-agent
	contextTimeout = defaultContextTimeout
	// update interval for token from zti-agent and dps-agent
	updateIntervalSecs int64 = defaultUpdateIntervalSecs
	// zti agent socket path
	agentSocketPath string

	// backward compatible with dps agent: need to deprecate in future version
	dpsAgentUser    string
	dpsAgentPSM     string
	dpsAgentEnabled string

	initializeCompleted bool

	defaultAgentSocketPaths = []string{"/var/run/zti-agent.sock", "/var/run/zti-agent/sockets/agent.sock"}
)

func refreshIdentity() (err error) {
	rwLock.RLock()
	defer rwLock.RUnlock()
	if !isEmptyString(globalToken) {
		s, err := syncInformation()
		if err != nil {
			return err
		}

		context, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return writeContentToMemfd(context)
	}
	return
}

func daemonRoutine() {
	defer unMapMemfd()
	for {
		if getInitializeCompleted() {
			if err := refreshSVID(time.Now().Unix()); err != nil {
				logrus.Error("security.zti.jwt_helper refreshSVID error: ", err.Error())
			}
		}
		if err := refreshIdentity(); err != nil {
			logrus.Error("security.zti.jwt_helper refreshIdentity error: ", err.Error())
		}
		time.Sleep(daemonUpdateInterval)
	}
}

// create the Go Rountine and update the token every 2 hours
func init() {
	initMetricCli()
	go daemonRoutine()
}

// set context timeout for workload api grpc call
func SetContextTimeout(timeout time.Duration) {
	contextTimeout = timeout
}

// set the token update interval
func SetUpdateIntervalSecs(seconds int64) {
	updateIntervalSecs = seconds
}

// set the token path
func SetTokenPath(path string) {
	rwLock.Lock()
	defer rwLock.Unlock()
	customTokenPath = path
}

// get the token path
func GetTokenPath() string {
	rwLock.RLock()
	defer rwLock.RUnlock()
	if !isEmptyString(customTokenPath) {
		return customTokenPath
	}
	return os.Getenv(tokenPathEnv)
}

// set the token string
func SetTokenStr(token string) {
	rwLock.Lock()
	defer rwLock.Unlock()
	customTokenStr = token
}

// get the token string
func GetTokenStr() string {
	rwLock.RLock()
	defer rwLock.RUnlock()
	if !isEmptyString(customTokenStr) {
		return customTokenStr
	}
	return os.Getenv(tokenStrEnv)
}

// Set the service identity to be obtained
func SetServiceIdentity(serviceIdentity string) {
	rwLock.Lock()
	defer rwLock.Unlock()
	customServiceIdentity = serviceIdentity
}

// Get the service identity to be obtained
func GetServiceIdentity() string {
	rwLock.RLock()
	defer rwLock.RUnlock()
	return customServiceIdentity
}

// get sdk initialization state
func getInitializeCompleted() (result bool) {
	result = initializeCompleted
	if !initializeCompleted {
		setInitializeCompleted()
	}

	return result
}

// set sdk initialization state
func setInitializeCompleted() {
	rwLock.Lock()
	defer rwLock.Unlock()
	initializeCompleted = true
}

func refreshAgentSocketPath() (diff bool) {

	targetAgentSocketPath := os.Getenv(agentSocketPathEnv)
	if !isEmptyString(targetAgentSocketPath) {
		if _, fileErr := os.Stat(targetAgentSocketPath); fileErr == nil {
			diff = targetAgentSocketPath != agentSocketPath
		}
	} else {
		for _, path := range defaultAgentSocketPaths {
			if _, fileErr := os.Stat(path); fileErr == nil {
				diff = path != agentSocketPath
				targetAgentSocketPath = path
				break
			}
		}
	}
	if diff {
		rwLock.Lock()
		defer rwLock.Unlock()
		agentSocketPath = targetAgentSocketPath
	}

	return diff
}

// refresh all environment variables when refreshSVID is called
func refreshEnviron() {
	tokenPath = GetTokenPath()
	tokenStr = GetTokenStr()
	dpsAgentEnabled = os.Getenv(dpsAgentEnablePath)
	dpsAgentPSM = os.Getenv(dpsAgentPSMEnv)
	dpsAgentUser = os.Getenv(dpsAgentUserEnv)
}

// refresh the token from token, dps-agent, token path or zti-agent
func refreshSVID(now int64) (err error) {
	routineMutex.Lock()
	defer routineMutex.Unlock()
	defer setUpdateTime(now)
	refreshEnviron()
	if !isEmptyString(tokenStr) {
		getTokenFromStr()
		EmitCounter(GetTokenFromString, nil)
		return err
	} else if dpsAgentEnabled == "1" && (!isEmptyString(dpsAgentPSM) || !isEmptyString(dpsAgentUser)) {
		if err = pullTokenFromDpsAgentDaemon(); err != nil {
			EmitCounter(GetTokenFromDPSAgent, nil)
		}
		return err
	} else if !isEmptyString(tokenPath) {
		if fallback, err := getTokenFromPath(); !fallback {
			if err != nil {
				EmitCounter(GetTokenFail, nil)
			}
			return err
		}
	}

	diff := refreshAgentSocketPath()
	if !isEmptyString(agentSocketPath) {
		if err = pullTokenFromDaemon(diff); err != nil {
			EmitCounter(GetTokenFail, nil)
		}
	}

	return err
}

func GetJwtSVID() (string, error) {
	now := time.Now().Unix()

	if now-getUpdateTime() >= updateIntervalSecs {
		if err := refreshSVID(now); err != nil {
			logrus.Error("security.zti.jwt_helper refreshSVID error: ", err.Error())
		}
		if err := refreshIdentity(); err != nil {
			logrus.Error("security.zti.jwt_helper refreshIdentity error: ", err.Error())
		}
	}

	rwLock.RLock()
	defer rwLock.RUnlock()
	if isEmptyString(globalToken) {
		EmitCounter(GetTokenFail, nil)
		return globalToken, errs.New("the token is empty")
	}
	return globalToken, nil
}

func IsTokenZTI(tokenString string) bool {
	token, err := jwt.ParseSigned(tokenString)
	if err != nil {
		return false
	}
	if len(token.Headers) < 1 || isEmptyString(token.Headers[0].KeyID) {
		return false
	}
	return true
}

// deprecated: will no longer support this API in the next release
func DecodeGDPRorJwtSVID(tokenString string) (*LegacyIdentity, error) {
	token, err := jwt.ParseSigned(tokenString)
	if err != nil {
		return nil, err
	}

	// gdpr token does not have KeyID in the token header
	if len(token.Headers) < 1 || isEmptyString(token.Headers[0].KeyID) {
		legacyId := &LegacyIdentity{}
		if err := token.UnsafeClaimsWithoutVerification(legacyId); err != nil {
			return legacyId, err
		}
		return legacyId, nil
	}

	// support decode JWT-SVID when received token string is JWT-SVID
	zti, err := decodeIdentityFromTokenString(tokenString)
	if err != nil {
		return nil, err
	}
	return zti.GetLegacyIdentityFromZTI()
}

// deprecated: will no longer support this API in the next release
func DecodeJwtSVID(tokenString string) (*ZeroTrustIdentity, error) {
	return decodeIdentityFromTokenString(tokenString)
}

// helper function to decode payload from token string
// validate the format of token and claims in the token
// note: expiry is not validated
func decodeIdentityFromTokenString(tokenString string) (*ZeroTrustIdentity, error) {
	token, err := jwt.ParseSigned(tokenString)
	if err != nil {
		return nil, err
	}

	var claims Claims
	if err := token.UnsafeClaimsWithoutVerification(&claims); err != nil {
		return nil, err
	}

	idString := claims.Subject
	spiffeID, err := spiffeid.FromString(idString)
	if err != nil {
		return nil, err
	}

	zti := &ZeroTrustIdentity{
		SpiffeID: spiffeID,
		Expiry:   claims.Expiry.Time(),
	}

	if err := zti.ValidateZTI(); err != nil {
		return nil, err
	}
	return zti, nil
}

func getTokenFromStr() {
	rwLock.Lock()
	globalToken = strings.TrimFunc(tokenStr, trimJwtToken)
	tokenSource = tokenSourceString
	rwLock.Unlock()
}

func enableFallBack(context string) bool {
	fallbackEnabled := os.Getenv(fallbackZTIAgentEnv)
	if fallbackEnabled == "1" {
		return true
	}

	return context == fallbackZTIAgentEnv
}

func getTokenFromPath() (fallback bool, err error) {

	if _, err = os.Stat(tokenPath); err != nil {
		EmitCounter(FetchTokenFromPathFail, nil)
		return enableFallBack(""), err
	}
	tokenBytes, err := ioutil.ReadFile(tokenPath)
	if err != nil {
		EmitCounter(FetchTokenFromPathFail, nil)
		return enableFallBack(""), err
	}

	tokenStr := strings.TrimFunc(string(tokenBytes), trimJwtToken)
	if enableFallBack(tokenStr) {
		return true, nil
	}

	rwLock.Lock()
	globalToken = tokenStr
	tokenSource = tokenSourcePath
	rwLock.Unlock()

	return
}

func pullTokenFromDaemon(diff bool) (err error) {
	if workloadClient == nil || diff {
		workloadClient, err = workloadapi.New(context.Background(), workloadapi.WithAddr("unix://"+agentSocketPath))
		if err != nil {
			EmitCounter(GetTokenFromAgentFail, nil)
			return err
		}
	}

	fetchCtx, cancel := context.WithTimeout(context.Background(), contextTimeout)
	defer cancel()
	jwtSVIDs, err := workloadClient.FetchJWTSVIDs(fetchCtx, jwtsvid.Params{
		Audience: defaultAudience,
	})

	if err != nil {
		EmitCounter(GetTokenFromAgentFail, nil)
		return err
	}

	serviceIdentity := customServiceIdentity
	if isEmptyString(serviceIdentity) {
		serviceIdentity = env.PSM()
	}

	for _, svid := range jwtSVIDs {
		zti := &ZeroTrustIdentity{
			SpiffeID: svid.ID,
			Expiry:   svid.Expiry,
		}
		if err := zti.ValidateZTI(); err != nil {
			continue
		}

		if serviceIdentity == env.PSMUnknown || serviceIdentity == zti.ID {
			EmitCounter(GetTokenSucceed, nil)
			rwLock.Lock()
			globalToken = strings.TrimFunc(svid.Marshal(), trimJwtToken)
			tokenSource = tokenSourceZTI
			rwLock.Unlock()
			return
		}
	}

	if len(jwtSVIDs) > 0 {
		EmitCounter(GetTokenSucceed, nil)
		rwLock.Lock()
		globalToken = strings.TrimFunc(jwtSVIDs[0].Marshal(), trimJwtToken)
		tokenSource = tokenSourceZTI
		rwLock.Unlock()
	}

	return
}

func pullTokenFromDpsAgentDaemon() (err error) {
	var unixAddr *net.UnixAddr
	unixAddr, _ = net.ResolveUnixAddr("unix", dpsAgentSocketPath)

	for i := 0; i < 1; i++ {
		conn, err := net.DialUnix("unix", nil, unixAddr)
		if nil != err {
			continue
		}

		var cmd CommandTag
		cmd.PSM = dpsAgentPSM
		cmd.Cmd = 1
		cmd.User = dpsAgentUser

		b, err := json.Marshal(cmd)
		if nil == err {
			conn.Write(b)
			conn.Write([]byte("\n"))
			reader := bufio.NewReader(conn)
			if nil != reader {
				msg, err := reader.ReadString('\n')
				if nil == err {
					if strings.Contains(msg, "\n") {
						rwLock.Lock()
						globalToken = strings.Split(msg, "\n")[0]
						tokenSource = tokenSourceDPS
						rwLock.Unlock()
						break // break the for loop, get rid of repeated grpc calls
					}
				}
			}
		}
		conn.Close()
	}
	return err
}

func getUpdateTime() int64 {
	rwLock.RLock()
	defer rwLock.RUnlock()
	return updateTime
}

func setUpdateTime(now int64) {
	rwLock.Lock()
	defer rwLock.Unlock()
	updateTime = now
}

func isEmptyString(str string) bool {
	return len(str) == 0
}

func trimJwtToken(r rune) bool {
	return r == '\n' || r == '\r' || r == '\t' || r == ' ' || r == '"' || r == '\''
}
