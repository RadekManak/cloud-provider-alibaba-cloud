package route

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlCfg "k8s.io/cloud-provider-alibaba-cloud/pkg/config"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/controller/helper"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/model"
	prvd "k8s.io/cloud-provider-alibaba-cloud/pkg/provider"
	"k8s.io/klog/v2"
	"net"
	"strings"
	"time"
)

var (
	createBackoff = wait.Backoff{
		Duration: 4 * time.Second,
		Steps:    3,
		Factor:   2,
		Jitter:   1,
	}
)

func createRouteForInstance(ctx context.Context, table, providerID, cidr string, providerIns prvd.IVPC) (*model.Route, error) {
	var (
		route    *model.Route
		innerErr error
	)
	err := wait.ExponentialBackoff(createBackoff, func() (bool, error) {
		route, innerErr = providerIns.CreateRoute(ctx, table, providerID, cidr)
		if innerErr != nil {
			if strings.Contains(innerErr.Error(), "not found") {
				klog.Infof("not found route %s", innerErr.Error())
				return true, nil
			}
			klog.Errorf("Backoff creating route: %s", innerErr.Error())
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return nil, fmt.Errorf("error create route for node %v, err: %v", providerID, innerErr)
	}
	return route, nil
}

func deleteRouteForInstance(ctx context.Context, table, providerID, cidr string, providerIns prvd.IVPC) error {
	klog.Infof("delete route for node: %v", providerID)
	return providerIns.DeleteRoute(ctx, table, providerID, cidr)
}

func getRouteTables(ctx context.Context, providerIns prvd.Provider) ([]string, error) {
	vpcId, err := providerIns.VpcID()
	if err != nil {
		return nil, fmt.Errorf("get vpc id from metadata error: %s", err.Error())
	}
	if ctrlCfg.CloudCFG.Global.RouteTableIDS != "" {
		return strings.Split(ctrlCfg.CloudCFG.Global.RouteTableIDS, ","), nil
	}
	tables, err := providerIns.ListRouteTables(ctx, vpcId)
	if err != nil {
		return nil, fmt.Errorf("alicloud: "+
			"can not found routetable by id[%s], error: %v", ctrlCfg.CloudCFG.Global.VpcID, err)
	}
	if len(tables) > 1 {
		return nil, fmt.Errorf("alicloud: "+
			"multiple route tables found by vpc id[%s], length(tables)=%d", ctrlCfg.CloudCFG.Global.VpcID, len(tables))
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("alicloud: no route tables found by vpc id[%s]", ctrlCfg.CloudCFG.Global.VpcID)
	}
	return tables, nil
}

func (r *ReconcileRoute) syncTableRoutes(ctx context.Context, table string, nodes *v1.NodeList) error {
	routes, err := r.cloud.ListRoute(ctx, table)
	if err != nil {
		return fmt.Errorf("error listing routes: %v", err)
	}

	var clusterCIDR *net.IPNet
	if ctrlCfg.ControllerCFG.ClusterCIDR != "" {
		_, clusterCIDR, err = net.ParseCIDR(ctrlCfg.ControllerCFG.ClusterCIDR)
		if err != nil {
			return fmt.Errorf("error parse cluster cidr %s: %s", ctrlCfg.ControllerCFG.ClusterCIDR, err)
		}
	}

	for _, route := range routes {
		contains, _, err := containsRoute(clusterCIDR, route.DestinationCIDR)
		if err != nil {
			klog.Errorf("error contains route %v <- %v, error %v ", clusterCIDR, route.DestinationCIDR, err)
			continue
		}
		if !contains {
			continue
		}
		if conflictWithNodes(route.DestinationCIDR, nodes) {
			klog.Infof("delete route %s, %s", route.Name, route.DestinationCIDR)
			if err = deleteRouteForInstance(ctx, table, route.ProviderId, route.DestinationCIDR, r.cloud); err != nil {
				klog.Errorf("Could not delete route %s %s from table %s, %s", route.Name, route.DestinationCIDR, table, err.Error())
				continue
			}
			klog.Infof("Delete route %s, %s from table %s SUCCESS.", route.Name, route.DestinationCIDR, table)
		}
	}
	for _, node := range nodes.Items {
		if !r.configRoutes || helper.HasExcludeLabel(&node) {
			continue
		}

		readyCondition, ok := helper.FindCondition(node.Status.Conditions, v1.NodeReady)
		if ok && readyCondition.Status == v1.ConditionUnknown {
			continue
		}

		prvdId := node.Spec.ProviderID
		if prvdId == "" {
			continue
		}

		_, ipv4RouteCidr, err := getIPv4RouteForNode(&node)
		if err != nil || ipv4RouteCidr == "" {
			continue
		}

		err = r.addRouteForNode(ctx, table, ipv4RouteCidr, prvdId, &node, routes)
		if err != nil {
			klog.Errorf("try create route error: %s", err.Error())
			r.record.Event(
				&node,
				v1.EventTypeWarning,
				"CreateRouteFailed",
				fmt.Sprintf("Create Route Failed for %s reason: %s", table, err),
			)
			continue
		}

		networkCondition, ok := helper.FindCondition(node.Status.Conditions, v1.NodeNetworkUnavailable)
		if !ok || networkCondition.Status != v1.ConditionFalse {
			if err := r.updateNetworkingCondition(ctx, &node, true); err != nil {
				klog.Errorf("update node %s network condition err: %s", node.Name, err.Error())
			}
		}
	}
	return nil
}

func conflictWithNodes(route string, nodes *v1.NodeList) bool {
	for _, node := range nodes.Items {
		ipv4Cidr, _, err := getIPv4RouteForNode(&node)
		if err != nil {
			klog.Errorf("error get ipv4 cidr from node: %v", node.Name)
			continue
		}
		if ipv4Cidr == nil {
			continue
		}
		_, contains, err := containsRoute(ipv4Cidr, route)
		if err != nil {
			klog.Errorf("error get conflict state from node: %v and route: %v", node.Name, route)
			continue
		}
		if contains {
			klog.Warningf("conflict route with node %v(%v) found, route: %v", node.Name, ipv4Cidr, route)
			return true
		}

	}
	return false
}

func findRoute(ctx context.Context, table, pvid, cidr string, cachedRoutes []*model.Route, providerIns prvd.IVPC) (*model.Route, error) {
	if pvid == "" && cidr == "" {
		return nil, fmt.Errorf("empty query condition")
	}
	if len(cachedRoutes) != 0 {
		for _, route := range cachedRoutes {
			if pvid != "" && cidr != "" {
				if route.DestinationCIDR == cidr && route.ProviderId == pvid {
					return route, nil
				}
			} else if pvid != "" {
				if route.ProviderId == pvid {
					return route, nil
				}
			} else if cidr != "" {
				if route.DestinationCIDR == cidr {
					return route, nil
				}
			}
		}
		return nil, nil
	}
	return providerIns.FindRoute(ctx, table, pvid, cidr)
}

func containsRoute(outside *net.IPNet, insideRoute string) (containsEqual bool, realContains bool, err error) {
	if outside == nil {
		// outside is nil, contains all route
		return true, true, nil
	}
	_, cidr, err := net.ParseCIDR(insideRoute)
	if err != nil {
		return false, false, fmt.Errorf("ignoring route %s, unparsable CIDR: %v", insideRoute, err)
	}

	if outside.String() == insideRoute {
		return true, false, nil
	}

	lastIP := make([]byte, len(cidr.IP))
	for i := range lastIP {
		lastIP[i] = cidr.IP[i] | ^cidr.Mask[i]
	}
	if !outside.Contains(cidr.IP) || !outside.Contains(lastIP) {
		return false, false, nil
	}
	return true, true, nil
}
