package server

import (
	"fmt"
	"time"
	"vu/ase/core/src/procutils"
	"vu/ase/core/src/services"
	"vu/ase/core/src/state"

	pb_core_messages "github.com/VU-ASE/rovercom/packages/go/core"

	zmq "github.com/pebbe/zmq4"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"
)

// Sets up the registration server (based on a req-rep client-server model). Other services can register themselves by making a request to
// this server, or request service statusses and tuning parameters.
func Serve(addr string, state *state.State) error {
	server, err := zmq.NewSocket(zmq.REP)
	if err != nil {
		log.Err(err).Msg("Failed to create server socket")
		return err
	}
	err = server.Bind(addr)
	if err != nil {
		return err
	}
	defer server.Close()

	// This goroutine will periodically check if services are still running, and clean them up if not
	go func() {
		for {
			time.Sleep(5 * time.Second)
			// Clean up all services that are no longer active
			state.UpdateServiceStatusses()
		}
	}()

	// Main receiver loop
	for {
		// Receive request
		msg, err := server.RecvBytes(0)
		if err != nil {
			log.Err(err).Msg("Failed to receive request")
			continue
		} else {
			log.Debug().Msg("Received request")

			res, err := handleMessage(msg, state)
			if err != nil {
				log.Err(err).Msg("Failed to handle message")

				// Send the error in a special error object that the client can handle
				errMsg, err := proto.Marshal(&pb_core_messages.CoreMessage{
					Msg: &pb_core_messages.CoreMessage_Error{
						Error: &pb_core_messages.Error{
							Message: err.Error(),
						},
					},
				})
				if err != nil {
					log.Err(err).Msg("Failed to marshal error message")
					// Best-effort, we send a string so that the client has *a* reply and can continue, but the client probably does not know how to handle it
					_, _ = server.SendMessage("Failed to marshal error message")
				} else {
					// Error object was created and marshalled, send it
					_, _ = server.SendBytes(errMsg, 0)
				}

			} else {
				// Try to marshal the response and send it
				resBytes, err := proto.Marshal(res)
				if err != nil {
					log.Err(err).Msg("Failed to marshal response")
					// Best-effort, we send a string so that the client has *a* reply and can continue, but the client probably does not know how to handle it
					_, _ = server.SendMessage("Failed to marshal response")
				} else {
					log.Debug().Msg("Sending response")
					_, _ = server.SendBytes(resBytes, 0)
				}
			}
		}
	}

}

// Handles a message received by the server, and returns response message that should be send back to the client
func handleMessage(msg []byte, state *state.State) (*pb_core_messages.CoreMessage, error) {
	// Unmarshal the wrapper
	parsedMessage := pb_core_messages.CoreMessage{}
	err := proto.Unmarshal(msg, &parsedMessage)
	if err != nil {
		return handleUnsupported()
	}

	// Let's see what we're dealing with
	switch {
	// Service registration
	case parsedMessage.GetService() != nil:
		{
			res, err := handleServiceRegistration(parsedMessage.GetService(), state)
			return &pb_core_messages.CoreMessage{
				Msg: &pb_core_messages.CoreMessage_Service{
					Service: res,
				},
			}, err
		}
	case parsedMessage.GetServiceInformationRequest() != nil:
		{
			res := handleServiceInformationRequest(parsedMessage.GetServiceInformationRequest(), state)
			return &pb_core_messages.CoreMessage{
				Msg: &pb_core_messages.CoreMessage_Service{
					Service: res,
				},
			}, nil
		}
	case parsedMessage.GetServiceStatusUpdate() != nil:
		{
			res, err := handleServiceStatusUpdate(parsedMessage.GetServiceStatusUpdate(), state)
			return &pb_core_messages.CoreMessage{
				Msg: &pb_core_messages.CoreMessage_Service{
					Service: res,
				},
			}, err
		}
	case parsedMessage.GetTuningState() != nil:
		{
			res, err := handleTuningStateUpsert(parsedMessage.GetTuningState(), state)
			return &pb_core_messages.CoreMessage{
				Msg: &pb_core_messages.CoreMessage_TuningState{
					TuningState: res,
				},
			}, err
		}
	case parsedMessage.GetTuningStateRequest() != nil:
		{
			res, err := handleTuningStateRequest(state)
			return &pb_core_messages.CoreMessage{
				Msg: &pb_core_messages.CoreMessage_TuningState{
					TuningState: res,
				},
			}, err
		}
	case parsedMessage.GetServiceListRequest() != nil:
		{
			res, err := handleServiceListRequest(state)
			return &pb_core_messages.CoreMessage{
				Msg: &pb_core_messages.CoreMessage_ServiceList{
					ServiceList: res,
				},
			}, err
		}
	case parsedMessage.GetServiceOrder() != nil:
		{
			return handleUnimplemented()
		}
	default:
		{
			return handleUnsupported()
		}
	}
}

//
// REQ-REP endpoint handlers
//

func handleServiceRegistration(msg *pb_core_messages.Service, state *state.State) (*pb_core_messages.Service, error) {
	log.Debug().Msg("[reqrep]: handling service registration")

	// Clean up all services that are no longer active
	state.UpdateServiceStatusses()

	msg.Status = pb_core_messages.ServiceStatus_REGISTERED

	// We can't register a service that is already registered (by name)
	s := state.GetService(msg.Identifier.Name)
	if s != nil && procutils.ProcessExists(int(s.Identifier.Pid)) {
		log.Warn().Str("service", s.Identifier.Name).Int("pid", int(s.Identifier.Pid)).Msg("Attempted to register service that was already registered and is still active")
		return nil, fmt.Errorf("Tried to register servicce '%s' but failed: this service is already registered and still running (running with PID %d) ", msg.Identifier.Name, s.Identifier.Pid)
	}

	// We can't register a service with tuning options that are already used by another service
	for _, o := range msg.Options {
		existingOption, existingService := state.GetServiceOption(o.Name)
		if existingOption != nil && existingService != nil {
			return nil, fmt.Errorf("Tried to register servicce '%s' but failed: the service option %s is already in use by service '%s'. Change the name of this option in the service.yaml of service '%s' or stop service '%s' (running with PID %d) ", msg.Identifier.Name, existingOption.Name, existingService.Identifier.Name, msg.Identifier.Name, existingService.Identifier.Name, existingService.Identifier.Pid)
		}
	}

	// The registration timestamp is necessary to fetch tuning states later
	msg.RegisteredAt = time.Now().UnixMilli()

	// Actually add to the list of services
	state.AddService(msg)

	// Broadcast the new service for everyone interested
	err := BroadcastMessage(state.PublisherSocket, &pb_core_messages.CoreMessage{
		Msg: &pb_core_messages.CoreMessage_Service{
			Service: msg,
		},
	})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to broadcast new service")
	}
	return msg, nil
}

func handleServiceInformationRequest(msg *pb_core_messages.ServiceInformationRequest, state *state.State) *pb_core_messages.Service {
	log.Debug().Msg("[reqrep]: handling service information request")

	requestedService := msg.GetRequested()
	if requestedService == nil {
		log.Warn().Msg("Received service information request without requested service")
		return &pb_core_messages.Service{
			Identifier: &pb_core_messages.ServiceIdentifier{
				Name: "unknown",
				Pid:  0,
			},
			Status: pb_core_messages.ServiceStatus_UNKNOWN,
		}
	}

	service := state.GetService(requestedService.Name)
	if service == nil {
		log.Warn().Str("service", requestedService.Name).Msg("Received service information request for unregistered service")
		return &pb_core_messages.Service{
			Identifier: requestedService,
			Status:     pb_core_messages.ServiceStatus_NOT_REGISTERED,
		}
	}

	// we found the service, get the status
	status := services.ServiceStatus(service)
	service.Status = status
	return service
}

func handleServiceStatusUpdate(msg *pb_core_messages.ServiceStatusUpdate, state *state.State) (*pb_core_messages.Service, error) {
	log.Debug().Msg("[reqrep]: handling service status update")

	//! there is no actual check if the sender is actually the service that is being updated
	return state.UpdateServiceStatus(msg.Service.Name, msg.Service.Pid, msg.Status)
}

func handleTuningStateUpsert(msg *pb_core_messages.TuningState, state *state.State) (*pb_core_messages.TuningState, error) {
	log.Debug().Msg("[reqrep]: handling tuning state upsert")

	mergedTuning := state.UpdateTuningState(msg)
	if mergedTuning == nil {
		log.Warn().Msg("Failed to upsert tuning state")
		return nil, fmt.Errorf("Failed to upsert tuning state")
	}

	log.Debug().Msgf("Tuning state updated, now has %d parameters", len(mergedTuning.DynamicParameters))

	// Broadcast the new tuning state for everyone interested
	err := BroadcastMessage(state.PublisherSocket, &pb_core_messages.CoreMessage{
		Msg: &pb_core_messages.CoreMessage_TuningState{
			TuningState: mergedTuning,
		},
	})
	if err != nil {
		log.Warn().Err(err).Msg("Failed to broadcast new tuning state")
	}

	return mergedTuning, nil
}

func handleTuningStateRequest(state *state.State) (*pb_core_messages.TuningState, error) {
	log.Debug().Msg("[reqrep]: handling tuning state request")

	tuning := state.GetTuningState()
	if tuning == nil {
		log.Warn().Msg("Received tuning state message, but no tuning state was found")
		return nil, fmt.Errorf("No tuning state found")
	}
	return tuning, nil
}

func handleServiceListRequest(state *state.State) (*pb_core_messages.ServiceList, error) {
	log.Debug().Msg("[reqrep]: handling service list request")

	state.UpdateServiceStatusses()
	services := state.Services
	if services == nil {
		log.Warn().Msg("Received service list request message, but no services were found")
		return nil, fmt.Errorf("No services found")
	}

	// marshal the service list
	return &pb_core_messages.ServiceList{
		Services: services,
	}, nil
}

func handleUnimplemented() (*pb_core_messages.CoreMessage, error) {
	return nil, fmt.Errorf("This endpoint is not implemented yet")
}

func handleUnsupported() (*pb_core_messages.CoreMessage, error) {
	return nil, fmt.Errorf("This endpoint is not supported, or you provided an unsupported message")
}
