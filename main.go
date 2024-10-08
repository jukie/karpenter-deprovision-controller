package main

import (
	"context"
	"flag"
	"github.com/jukie/karpenter-deprovision-controller/pkg/clienthelpers"
	"github.com/jukie/karpenter-deprovision-controller/pkg/controller"
	"github.com/jukie/karpenter-deprovision-controller/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/klog/v2"
	"os/signal"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"syscall"
	"time"
)

var (
	dryRun     bool
	syncPeriod = 60 * time.Minute
	opts       = client.Options{}
)

// initializes klog and prometheus metrics, then parses command-line flags.
func initFlags() {
	flag.BoolVar(&dryRun, "dry-run", true, "Whether or not to execute do-not-disrupt pod annotation removals. Defaults to true")
	flag.Parse()
	klog.Infoln("Parsed Flags:")
	flag.Visit(func(f *flag.Flag) {
		klog.Infof("%s: %v", f.Name, f.Value)
	})

	if dryRun {
		opts.DryRun = &dryRun
		klog.Infoln("Dry-run mode enabled, resource update operations will not be applied")
	}
}

func main() {
	initFlags()
	metrics.Register()
	mgr, err := ctrlruntime.NewManager(clienthelpers.GetConfig(), ctrlruntime.Options{
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Event{}: {
					Field: fields.SelectorFromSet(fields.Set{
						"involvedObject.kind": controller.DisruptionBlockedEventKind,
						"reason":              controller.DisruptionBlockedEventReason,
					}),
				},
			},
		},
		NewCache: clienthelpers.NewCache,
		Client:   opts,
	})
	if err != nil {
		klog.Fatalf("Error creating Controller Manager: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	logger := klog.NewKlogr()
	log.IntoContext(ctx, logger)
	log.SetLogger(logger)

	nController := &controller.DeprovisionController{Client: mgr.GetClient()}
	if err := nController.Register(context.Background(), mgr); err != nil {
		klog.Fatalf("unable to register controller: %v", err)
	}
	if err := mgr.Start(ctx); err != nil {
		klog.Fatalf("unable to start manager: %v", err)
	}
}
