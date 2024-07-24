package services

import (
	"vu/ase/core/src/procutils"

	pb_systemmanager_messages "github.com/VU-ASE/pkg-CommunicationDefinitions/v2/packages/go/systemmanager"
)

// This function will check the current status of the service. This is done not only by checking the officially registered status, but also by checking if the process is still running.
func ServiceStatus(service *pb_systemmanager_messages.Service) pb_systemmanager_messages.ServiceStatus {
	if service == nil || service.Identifier == nil {
		return pb_systemmanager_messages.ServiceStatus_UNKNOWN
	}

	if !procutils.ProcessExists(int(service.Identifier.Pid)) {
		return pb_systemmanager_messages.ServiceStatus_STOPPED
	}

	return service.Status
}

func OptionTypeToString(optionType pb_systemmanager_messages.ServiceOption_Type) string {
	switch optionType {
	case pb_systemmanager_messages.ServiceOption_INT:
		return "int"
	case pb_systemmanager_messages.ServiceOption_FLOAT:
		return "float"
	case pb_systemmanager_messages.ServiceOption_STRING:
		return "string"
	default:
		return "unknown"
	}
}
