package cel

import (
	"fmt"

	json "code.byted.org/bcc/conf_engine/jsoniter"
)

func GetBytecode(cList []*Conf) ([]byte, error) {

	if err := CListMarshalWithDefaultEnv(cList); err != nil {
		return nil, fmt.Errorf("CListMarshalWithDefaultEnv error: %v", err)
	}
	ret, err := json.Marshal(&cList)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %v", err)
	}
	return ret, nil
}
