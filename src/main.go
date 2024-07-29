package main

import (
	"os"
	"vu/ase/core/src/procutils"
	"vu/ase/core/src/server"
	"vu/ase/core/src/state"

	pb_core_messages "github.com/VU-ASE/rovercom/packages/go/core"
	roverlib "github.com/VU-ASE/roverlib/src"
	"github.com/rs/zerolog/log"
)

// Use as a global variable so that the onTerminate callback function can call it
var systemState state.State

// The actual program
func run(service roverlib.ResolvedService, coreInfo roverlib.CoreInfo, initialTuningState *pb_core_messages.TuningState) error {
	// Create the broadcast pub/sub socket
	// first get the address to output on, defined in our service.yaml
	broadcastAddr, err := service.GetOutputAddress("broadcast")
	if err != nil {
		return err
	}
	pubsubSocket, err := server.SetupBroadcast(broadcastAddr)
	if err != nil {
		return err
	}
	defer pubsubSocket.Close()

	// Create the state, so that other services can use pubsubSocket
	systemState = state.State{
		Services:        make(state.ServiceList, 0),
		PublisherSocket: pubsubSocket,
		TuningState: &pb_core_messages.TuningState{
			Timestamp:         0,
			DynamicParameters: []*pb_core_messages.TuningState_Parameter{},
		},
	}

	// Get the address to listen on, defined in our service.yaml
	reqrepAddr, err := service.GetOutputAddress("server")
	if err != nil {
		return err
	}

	// We want the system manager to add its own service to the list of services, so that other services can find it
	systemState.AddService(&pb_core_messages.Service{
		Identifier: &pb_core_messages.ServiceIdentifier{
			Name: service.Name,
			Pid:  int32(os.Getpid()),
		},
		Endpoints: []*pb_core_messages.ServiceEndpoint{
			{
				Name:    "server",
				Address: reqrepAddr,
			},
			{
				Name:    "broadcast",
				Address: broadcastAddr,
			},
		},
		Status: pb_core_messages.ServiceStatus_RUNNING,
	})

	// Now run the main req/rep server loop, which can use the publisher socket to broadcast messages
	return server.Serve(reqrepAddr, &systemState)
}

func onTerminate(signal os.Signal) {
	log.Info().Msg("Gracefully terminating system manager")

	// send SIGTERM to all services
	for _, service := range systemState.Services {
		log.Info().Msgf("Killing '%v' process", service.Identifier.Name)
		err := procutils.KillProcess(int(service.Identifier.Pid))
		if err != nil {
			log.Err(err).Msgf("Failed to kill '%v' process", service.Identifier.Name)
		} else {
			log.Info().Msgf("Killed '%v' process", service.Identifier.Name)
		}
	}
}

func onTuningState(newTuning *pb_core_messages.TuningState) {
	// we ignore the tuning state, since we don't rely on any tuning parameters
}

// Used to start the program with the correct arguments
func main() {
	roverlib.Run(run, onTuningState, onTerminate, true)
}
