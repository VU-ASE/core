package state

import (
	pb_systemmanager_messages "github.com/VU-ASE/rovercom/packages/go/core"
)

func findParameter(key string, params []*pb_systemmanager_messages.TuningState_Parameter) *pb_systemmanager_messages.TuningState_Parameter {
	for _, param := range params {
		if param.GetString_() != nil && param.GetString_().Key == key {
			return param
		} else if param.GetInt() != nil && param.GetInt().Key == key {
			return param
		} else if param.GetFloat() != nil && param.GetFloat().Key == key {
			return param
		}
	}

	return nil
}

// Parses a parameter from the tuning state and returns the key
func getKeyAndType(param *pb_systemmanager_messages.TuningState_Parameter) (string, string) {
	if param.GetString_() != nil {
		return param.GetString_().Key, "string"
	} else if param.GetInt() != nil {
		return param.GetInt().Key, "int"
	} else if param.GetFloat() != nil {
		return param.GetFloat().Key, "float"
	}
	return "", ""
}

// This converts a service option object to a tuning state dynamic parameter object
func convertOptionToDynamicParameter(opt *pb_systemmanager_messages.ServiceOption) *pb_systemmanager_messages.TuningState_Parameter {
	if opt == nil {
		return nil
	}
	switch opt.Type {
	case pb_systemmanager_messages.ServiceOption_INT:
		return &pb_systemmanager_messages.TuningState_Parameter{
			Parameter: &pb_systemmanager_messages.TuningState_Parameter_Int{
				Int: &pb_systemmanager_messages.TuningState_Parameter_IntParameter{
					Key:   opt.Name,
					Value: int64(opt.GetIntDefault()),
				},
			},
		}
	case pb_systemmanager_messages.ServiceOption_FLOAT:
		return &pb_systemmanager_messages.TuningState_Parameter{
			Parameter: &pb_systemmanager_messages.TuningState_Parameter_Float{
				Float: &pb_systemmanager_messages.TuningState_Parameter_FloatParameter{
					Key:   opt.Name,
					Value: opt.GetFloatDefault(),
				},
			},
		}
	case pb_systemmanager_messages.ServiceOption_STRING:
		return &pb_systemmanager_messages.TuningState_Parameter{
			Parameter: &pb_systemmanager_messages.TuningState_Parameter_String_{
				String_: &pb_systemmanager_messages.TuningState_Parameter_StringParameter{
					Key:   opt.Name,
					Value: opt.GetStringDefault(),
				},
			},
		}
	default:
		return nil
	}
}

// Takes in a service option object and a tuning parameter and returns true if the names (keys) are equal but the types are not
func optionMismatchesParameter(opt *pb_systemmanager_messages.ServiceOption, param *pb_systemmanager_messages.TuningState_Parameter) bool {
	if opt == nil || param == nil {
		return false
	}
	key, keyType := getKeyAndType(param)
	if key != opt.Name {
		return false
	}
	switch opt.Type {
	case pb_systemmanager_messages.ServiceOption_INT:
		return keyType != "int"
	case pb_systemmanager_messages.ServiceOption_FLOAT:
		return keyType != "float"
	case pb_systemmanager_messages.ServiceOption_STRING:
		return keyType != "string"
	default:
		return false
	}
}
