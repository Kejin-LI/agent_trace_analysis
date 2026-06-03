package model

type VarType int32

const (
	VarTypeInt    VarType = 0
	VarTypeFloat  VarType = 1
	VarTypeString VarType = 2
	VarTypeList   VarType = 3
	VarTypeDict   VarType = 4

	VarTypeBool    VarType = 9
	VarTypeUnknown VarType = 255
)

type VarStatus int32

const (
	VarStatusInvalid VarStatus = 0
	VarStatusValid   VarStatus = 1
)
