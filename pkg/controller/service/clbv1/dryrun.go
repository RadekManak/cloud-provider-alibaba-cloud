package clbv1

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/dryrun"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/util"
	"k8s.io/klog/v2"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
)

var initial = sync.Map{}

func initMap(client client.Client) {
	svcs := v1.ServiceList{}
	err := client.List(context.TODO(), &svcs)
	if err != nil {
		klog.Infof("init Map error: %s", err.Error())
		os.Exit(1)
	}

	length := 0
	for _, m := range svcs.Items {
		if !needAdd(&m) {
			continue
		}
		length++
		initial.Store(util.NamespacedName(&m).String(), 0)
	}
	util.ServiceLog.Info("ccm initial process finished.", "length", length)
	if length == 0 {
		err := dryrun.ResultEvent(client, dryrun.SUCCESS, "ccm initial process finished")
		if err != nil {
			util.ServiceLog.Error(err, "fail to write precheck event")
		}
		os.Exit(0)
	}
}

func mapfull() bool {
	total, unsync := 0, 0
	initial.Range(
		func(key, value interface{}) bool {
			val, ok := value.(int)
			if !ok {
				// not supposed
				return true
			}
			if val != 1 {
				unsync += 1
			}
			total += 1
			return true
		},
	)
	util.ServiceLog.Info("Reconcile process", "total", total, "unsync", unsync)
	return unsync == 0
}
