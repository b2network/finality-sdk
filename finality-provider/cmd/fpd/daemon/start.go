package daemon

import (
	"context"
	"fmt"
	"github.com/babylonlabs-io/finality-gadget/db"
	"github.com/babylonlabs-io/finality-provider/clientcontroller/generic/finalitygadget"
	"github.com/gin-gonic/gin"
	"net"
	"path/filepath"

	"github.com/babylonlabs-io/babylon/types"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/lightningnetwork/lnd/signal"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	clsservice "github.com/babylonlabs-io/finality-provider/clientcontroller/generic/service"
	fpcmd "github.com/babylonlabs-io/finality-provider/finality-provider/cmd"
	fpcfg "github.com/babylonlabs-io/finality-provider/finality-provider/config"
	"github.com/babylonlabs-io/finality-provider/finality-provider/service"
	"github.com/babylonlabs-io/finality-provider/log"
	"github.com/babylonlabs-io/finality-provider/util"
)

// CommandStart returns the start command of fpd daemon.
func CommandStart() *cobra.Command {
	var cmd = &cobra.Command{
		Use:     "start",
		Short:   "Start the finality-provider app daemon.",
		Long:    `Start the finality-provider app. Note that eotsd should be started beforehand`,
		Example: `fpd start --home /home/user/.fpd`,
		Args:    cobra.NoArgs,
		RunE:    fpcmd.RunEWithClientCtx(runStartCmd),
	}
	cmd.Flags().String(fpEotsPkFlag, "", "The EOTS public key of the finality-provider to start")
	cmd.Flags().String(passphraseFlag, "", "The pass phrase used to decrypt the private key")
	cmd.Flags().String(rpcListenerFlag, "", "The address that the RPC server listens to")
	return cmd
}

func runStartCmd(ctx client.Context, cmd *cobra.Command, args []string) error {
	homePath, err := filepath.Abs(ctx.HomeDir)
	if err != nil {
		return err
	}
	homePath = util.CleanAndExpandPath(homePath)
	flags := cmd.Flags()

	fpStr, err := flags.GetString(fpEotsPkFlag)
	if err != nil {
		return fmt.Errorf("failed to read flag %s: %w", fpEotsPkFlag, err)
	}

	rpcListener, err := flags.GetString(rpcListenerFlag)
	if err != nil {
		return fmt.Errorf("failed to read flag %s: %w", rpcListenerFlag, err)
	}

	passphrase, err := flags.GetString(passphraseFlag)
	if err != nil {
		return fmt.Errorf("failed to read flag %s: %w", passphraseFlag, err)
	}

	cfg, err := fpcfg.LoadConfig(homePath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if rpcListener != "" {
		_, err := net.ResolveTCPAddr("tcp", rpcListener)
		if err != nil {
			return fmt.Errorf("invalid RPC listener address %s, %w", rpcListener, err)
		}
		cfg.RpcListener = rpcListener
	}

	logger, err := log.NewRootLoggerWithFile(fpcfg.LogFile(homePath), cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to initialize the logger: %w", err)
	}

	dbBackend, err := cfg.DatabaseConfig.GetDbBackend()
	if err != nil {
		return fmt.Errorf("failed to create db backend: %w", err)
	}

	// Init local DB for storing and querying blocks
	db, err := db.NewBBoltHandler(cfg.DBFilePath, logger)
	if err != nil {
		return fmt.Errorf("failed to create DB handler: %w", err)
	}
	defer db.Close()
	err = db.CreateInitialSchema()
	if err != nil {
		return fmt.Errorf("create initial buckets error: %w", err)
	}

	fg, err := finalitygadget.NewFinalityGadgetCustom(cfg.FGConfig, db, logger)
	if err != nil {
		return fmt.Errorf("failed to create finality gadget: %w", err)
	}

	// Create a cancellable context
	fgCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Start monitoring BTC staking activation
	go fg.MonitorBtcStakingActivation(fgCtx)

	fpApp, err := loadApp(logger, cfg, dbBackend, fg)
	if err != nil {
		return fmt.Errorf("failed to load app: %w", err)
	}

	if err := startApp(fpApp, fpStr, passphrase); err != nil {
		return fmt.Errorf("failed to start app: %w", err)
	}

	// Hook interceptor for os signals.
	shutdownInterceptor, err := signal.Intercept()
	if err != nil {
		return err
	}

	if cfg.ChainType == "generic" {
		go func() {
			fsdk := clsservice.NewFinalitySDKHandler(cfg.ConsumerGenericConfig.ServiceRPC, fg)

			router := gin.Default()
			router.GET("/api/v1/block/:height", fsdk.QueryData)
			err := router.Run(":" + cfg.APIPort)
			if err != nil {
				logger.Error("failed to start API server", zap.Error(err))
			}
		}()
	}

	fpServer := service.NewFinalityProviderServer(cfg, logger, fpApp, dbBackend, shutdownInterceptor)
	return fpServer.RunUntilShutdown()

}

// loadApp initialize an finality provider app based on config and flags set.
func loadApp(
	logger *zap.Logger,
	cfg *fpcfg.Config,
	dbBackend walletdb.DB,
	fg *finalitygadget.FinalityGadgetCustom,
) (*service.FinalityProviderApp, error) {
	fpApp, err := service.NewFinalityProviderAppFromConfig(cfg, dbBackend, logger, fg)
	if err != nil {
		return nil, fmt.Errorf("failed to create finality-provider app: %v", err)
	}

	return fpApp, nil
}

// startApp starts the app and the handle of finality providers if needed based on flags.
func startApp(
	fpApp *service.FinalityProviderApp,
	fpPkStr, passphrase string,
) error {
	// only start the app without starting any finality-provider instance
	// as there might be no finality-provider registered yet
	if err := fpApp.Start(); err != nil {
		return fmt.Errorf("failed to start the finality-provider app: %w", err)
	}

	if fpPkStr != "" {
		// start the finality-provider instance with the given public key
		fpPk, err := types.NewBIP340PubKeyFromHex(fpPkStr)
		if err != nil {
			return fmt.Errorf("invalid finality-provider public key %s: %w", fpPkStr, err)
		}

		if err := fpApp.StartHandlingFinalityProvider(fpPk, passphrase); err != nil {
			return fmt.Errorf("failed to start the finality-provider instance %s: %w", fpPkStr, err)
		}
	}

	return fpApp.StartHandlingAll()
}
