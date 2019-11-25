package main

import (
	"context"
	"fmt"
	"github.com/makeitplay/arena/units"
	clientGo "github.com/makeitplay/client-player-go"
	"github.com/makeitplay/client-player-go/ops"
	"github.com/makeitplay/client-player-go/proto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"os"
	"os/signal"
)

var logger *zap.SugaredLogger
var playerClient ops.Client
var playerCtx context.Context
var playerConfig clientGo.Config

func init() {
	configZap := zap.NewDevelopmentConfig()
	configZap.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapLog, err := configZap.Build()
	if err != nil {
		log.Fatalf("could not initiliase looger: %s", err)
	}
	logger = zapLog.Sugar()
}

func init() {
	var err error
	playerConfig, err = clientGo.LoadConfig("./config.json")
	if err != nil {
		logger.Fatalf("did not load the config: %s", err)
	}
	if err := playerConfig.ParseConfigFlags(); err != nil {
		logger.Fatalf("did not parsed well the flags for config: %s", err)
	}

	logger = logger.Named(fmt.Sprintf("%s-%d", playerConfig.TeamSide, playerConfig.Number))
}

func main() {
	var err error
	// just creating a position based on the player number
	playerConfig.InitialPosition = proto.Point{
		X: units.FieldWidth / 4,
		Y: int32(playerConfig.Number) * proto.PlayerSize * 2, //(units.FieldHeight / 4) - (pos * units.PlayerSize),
	}

	if playerConfig.TeamSide == proto.Team_AWAY {
		playerConfig.InitialPosition.X = proto.FieldWidth - playerConfig.InitialPosition.X
	}

	playerCtx, playerClient, err = clientGo.NewClient(playerConfig)
	if err != nil {
		logger.Fatalf("did not connected to the gRPC server at '%s': %s", playerConfig.GRPCAddress, err)
	}
	playerClient.OnNewTurn(myDecider, logger.Named("client"))

	// keep the process alive
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	select {
	case <-signalChan:
		logger.Warn("got interruption signal")
		if err := playerClient.Stop(); err != nil {
			logger.Errorf("error stopping the player client: %s", err)
		}
	case <-playerCtx.Done():
		logger.Infof("player client stopped")
	}
	logger.Infof("process finished")
}

func myDecider(snapshot *proto.GameSnapshot, sender ops.OrderSender) {
	me := proto.GetPlayer(snapshot, playerConfig.TeamSide, playerConfig.Number)
	if me == nil {
		logger.Fatalf("i did not find my self in the game")
		return
	}
	var orders []proto.PlayerOrder
	// we are going to kick the ball as soon as we catch it
	if proto.IsBallHolder(snapshot, me) {
		orderToKick, err := proto.MakeOrderKick(*snapshot.Ball, proto.GetOpponentGoal(me.TeamSide).Center, units.BallMaxSpeed)
		if err != nil {
			logger.Warnf("could not create kick order during turn %d: %s", snapshot.Turn, err)
			return
		}
		orders = []proto.PlayerOrder{orderToKick}
	} else if me.Number == 10 {
		// otherwise, let's run towards the ball like kids
		orderToMove, err := proto.MakeOrderMoveMaxSpeed(*me.Position, *snapshot.Ball.Position)
		if err != nil {
			logger.Warnf("could not create move order during turn %d: %s", snapshot.Turn, err)
			return
		}
		orders = []proto.PlayerOrder{orderToMove, proto.MakeOrderCatch()}
	} else {
		orders = []proto.PlayerOrder{proto.MakeOrderCatch()}
	}

	resp, err := sender.Send(playerCtx, orders, "")
	if err != nil {
		logger.Warnf("could not send kick order during turn %d: %s", snapshot.Turn, err)
	} else if resp.Code != proto.OrderResponse_SUCCESS {
		logger.Warnf("order sent not  order during turn %d: %s", snapshot.Turn, err)
	}
}
