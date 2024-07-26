package state

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"vu/ase/core/src/procutils"
	"vu/ase/core/src/services"

	pb_systemmanager_messages "github.com/VU-ASE/rovercom/packages/go/core"
	zmq "github.com/pebbe/zmq4"
	"github.com/rs/zerolog/log"
)

// A list of all registered services
type ServiceList []*pb_systemmanager_messages.Service

type State struct {
	Services        ServiceList
	PublisherSocket *zmq.Socket
	TuningState     *pb_systemmanager_messages.TuningState
}

func (state *State) GetService(name string) *pb_systemmanager_messages.Service {
	for _, s := range state.Services {
		if s != nil && strings.EqualFold(s.Identifier.Name, name) {
			return s
		}
	}
	return nil
}

func (state *State) AddService(service *pb_systemmanager_messages.Service) {
	if service != nil {
		log.Info().Str("name", service.Identifier.Name).Int32("pid", service.Identifier.Pid).Msg("Added service")
		state.Services = append(state.Services, service)
	}
}

func (state *State) UpdateServiceStatus(name string, pid int32, status pb_systemmanager_messages.ServiceStatus) (*pb_systemmanager_messages.Service, error) {
	for _, s := range state.Services {
		if s != nil && strings.EqualFold(s.Identifier.Name, name) && s.Identifier.Pid == pid {
			s.Status = status
			return s, nil
		}
	}

	return nil, fmt.Errorf("Could not find service '%s' and pid '%d' to update status for", name, pid)
}

func (state *State) RemoveService(name string, pid int32) {
	state.Services = slices.DeleteFunc(
		state.Services,
		func(s *pb_systemmanager_messages.Service) bool {
			if s == nil {
				return true
			}
			removed := strings.EqualFold(s.Identifier.Name, name) && s.Identifier.Pid == pid
			if removed {
				log.Info().Str("name", name).Int32("pid", pid).Msg("Removed service")
			}
			return removed
		},
	)
}

// Iterates over all services and checks if they have a tuning option with the given key and returns the first one found (there should be 0 or 1, but not more)
func (state *State) GetServiceOption(key string) (*pb_systemmanager_messages.ServiceOption, *pb_systemmanager_messages.Service) {
	for _, s := range state.Services {
		if s != nil && s.Options != nil && procutils.ProcessExists(int(s.Identifier.Pid)) {
			for _, o := range s.Options {
				if o != nil && o.Name == key {
					return o, s
				}
			}
		}
	}

	return nil, nil
}

// This function will go over the entire list of services and update their status according to the current state of the system (using systemctl).
func (state *State) UpdateServiceStatusses() {
	for _, s := range state.Services {
		if s != nil {
			s.Status = services.ServiceStatus(s)
		}
	}
	// delete all stopped services
	state.Services = slices.DeleteFunc(
		state.Services,
		func(s *pb_systemmanager_messages.Service) bool {
			delete := s.Status == pb_systemmanager_messages.ServiceStatus_STOPPED || s.Status == pb_systemmanager_messages.ServiceStatus_NOT_REGISTERED || s.Status == pb_systemmanager_messages.ServiceStatus_UNKNOWN
			if delete {
				log.Info().Str("name", s.Identifier.Name).Int32("pid", s.Identifier.Pid).Msg("Removed service")
			}
			return delete
		},
	)
}

// This will replace the current tuning state with a new one, and return the new tuning state
// it will *not* merge the tuning state with the old one, but replace it entirely
func (state *State) UpdateTuningState(ts *pb_systemmanager_messages.TuningState) *pb_systemmanager_messages.TuningState {
	log.Info().Msg("Updating tuning state")

	// Set the timestampp, so that it can be compared to local options later
	ts.Timestamp = uint64(time.Now().UnixMilli())

	// Delete the parameters that are not valid
	// - because they have a type that does not match the type of a service option
	ts.DynamicParameters = slices.DeleteFunc(ts.DynamicParameters, func(p *pb_systemmanager_messages.TuningState_Parameter) bool {
		for _, s := range state.Services {
			for _, o := range s.Options {
				if optionMismatchesParameter(o, p) {
					log.Debug().Msgf("Deleting parameter %s because it does not match any service option", o.Name)
					return true
				}
			}
		}

		return false
	})

	state.TuningState = ts
	return state.GetTuningState()
}

// This will fetch the tuning state and compared it with the registered services.
// If a service registered later than the latest tuning state, its service.yaml values take precedence.
// Otherwise, the tuning state values take precedence, unless the service has declared a value as read-only (non-mutable)
func (state *State) GetTuningState() *pb_systemmanager_messages.TuningState {
	// We will not modify the saved tuning state, but create a new object with all combined values
	combinedTuning := pb_systemmanager_messages.TuningState{
		// This will be filled with the newly decided parameters
		DynamicParameters: make([]*pb_systemmanager_messages.TuningState_Parameter, 0),
		Timestamp:         uint64(time.Now().UnixMilli()),
	}

	// Get the old tuning state and put the old parameters in an array so that we can eliminate nill checks
	latest := state.TuningState
	oldParams := make([]*pb_systemmanager_messages.TuningState_Parameter, 0)
	if latest != nil {
		oldParams = latest.DynamicParameters
	}

	// Go over all services and check if they have tuning options that are not in the tuning state, or if they registered later than the tuning state was last updated
	for _, s := range state.Services {
		for _, o := range s.Options {
			// Try to find the original tuning parameter in the tuning state
			existingParam := findParameter(o.Name, oldParams)
			// Override/fill with the option value if:
			// - the option is not mutable (read-only)
			// - the parameter is missing from the tuning state
			// - there is no tuning state yet
			// - the service was registered after the latest tuning state
			// - the option type does not match the tuning state type
			if !o.Mutable || existingParam == nil || latest == nil || uint64(s.RegisteredAt) > latest.Timestamp || optionMismatchesParameter(o, existingParam) {
				// Add the option to the tuning state
				cvt := convertOptionToDynamicParameter(o)
				log.Debug().Msgf("Converted option to dynamic parameter: %s", cvt.String())
				if cvt != nil {
					combinedTuning.DynamicParameters = append(combinedTuning.DynamicParameters, cvt)
				} else {
					log.Warn().Str("option", o.Name).Msg("Failed to convert option to dynamic parameter. This should never happen!")
				}
			}
		}
	}

	// Add all old parameters that are still valid (i.e. not already in the combined tuning state)
	for _, op := range oldParams {
		if slices.ContainsFunc(combinedTuning.DynamicParameters, func(np *pb_systemmanager_messages.TuningState_Parameter) bool {
			oldKey, _ := getKeyAndType(op)
			newKey, _ := getKeyAndType(np)
			return oldKey == newKey
		}) {
			log.Debug().Msgf("Parameter %s already in combined tuning state", op.String())
		} else {
			log.Debug().Msgf("Adding old parameter %s to combined tuning state", op.String())
			combinedTuning.DynamicParameters = append(combinedTuning.DynamicParameters, op)
		}
	}

	log.Debug().Msgf("Returning updated tuning state %s (%d params)", combinedTuning.String(), len(combinedTuning.DynamicParameters))
	return &combinedTuning
}
