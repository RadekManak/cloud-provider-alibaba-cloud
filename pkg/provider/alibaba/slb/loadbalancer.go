package slb

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/cloud-provider-alibaba-cloud/pkg/model"
	prvd "k8s.io/cloud-provider-alibaba-cloud/pkg/provider"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/base"
	"k8s.io/cloud-provider-alibaba-cloud/pkg/provider/alibaba/util"
	"k8s.io/klog/v2"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/utils"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/slb"
)

func NewLBProvider(
	auth *base.ClientMgr,
) *SLBProvider {
	return &SLBProvider{auth: auth}
}

var _ prvd.ILoadBalancer = &SLBProvider{}

type SLBProvider struct {
	auth *base.ClientMgr
}

func (p SLBProvider) FindLoadBalancer(ctx context.Context, mdl *model.LoadBalancer) error {

	// 1. find by loadbalancer id
	if mdl.LoadBalancerAttribute.LoadBalancerId != "" {
		klog.Infof("[%s] try to find loadbalancer by id %s",
			mdl.NamespacedName, mdl.LoadBalancerAttribute.LoadBalancerId)
		return p.DescribeLoadBalancer(ctx, mdl)
	}

	// 2. find by tags
	items, err := json.Marshal(mdl.LoadBalancerAttribute.Tags)
	if err != nil {
		return fmt.Errorf("tags marshal error: %s", err.Error())
	}

	klog.Infof("[%s] try to find loadbalancer by tag %s", mdl.NamespacedName, string(items))
	req := slb.CreateDescribeLoadBalancersRequest()
	req.Tags = string(items)
	resp, err := p.auth.SLB.DescribeLoadBalancers(req)
	if err != nil {
		return fmt.Errorf("[%s] find loadbalancer by tag error: %s", mdl.NamespacedName, util.FormatErrorMessage(err))
	}
	klog.V(5).Infof("RequestId: %s, API: %s", resp.RequestId, "DescribeLoadBalancers")

	num := len(resp.LoadBalancers.LoadBalancer)
	if num > 0 {
		if len(resp.LoadBalancers.LoadBalancer) > 1 {
			klog.Infof("[%s] find [%d] load balances, use the first one", mdl.NamespacedName, num)
		}
		// TODO Remove DescribeLoadBalancer
		//  because DescribeLoadBalances do not return deleteprotection param, we need to call DescribeLoadBalancer() func
		mdl.LoadBalancerAttribute.LoadBalancerId = resp.LoadBalancers.LoadBalancer[0].LoadBalancerId
		return p.DescribeLoadBalancer(ctx, mdl)
	}

	// 3. find by loadbalancer name
	klog.Infof("[%s] try to find loadbalancer by name %s",
		mdl.NamespacedName, mdl.LoadBalancerAttribute.LoadBalancerName)
	if mdl.LoadBalancerAttribute.LoadBalancerName == "" {
		klog.Warningf("[%s] find loadbalancer by name error: loadbalancer name is empty.", mdl.NamespacedName.String())
		return nil
	}
	req = slb.CreateDescribeLoadBalancersRequest()
	req.LoadBalancerName = mdl.LoadBalancerAttribute.LoadBalancerName
	resp, err = p.auth.SLB.DescribeLoadBalancers(req)
	if err != nil {
		return fmt.Errorf("[%s] find loadbalancer by name error: %s", mdl.NamespacedName, util.FormatErrorMessage(err))
	}
	num = len(resp.LoadBalancers.LoadBalancer)
	if num > 0 {
		if len(resp.LoadBalancers.LoadBalancer) > 1 {
			klog.Infof("[%s] find [%d] load balances, use the first one", mdl.NamespacedName, num)
		}
		mdl.LoadBalancerAttribute.LoadBalancerId = resp.LoadBalancers.LoadBalancer[0].LoadBalancerId
		// TODO Remove DescribeLoadBalancer
		//  because DescribeLoadBalances do not return deleteprotection param, we need to call DescribeLoadBalancer() func
		return p.DescribeLoadBalancer(ctx, mdl)
	}

	return nil
}

func (p SLBProvider) CreateLoadBalancer(ctx context.Context, mdl *model.LoadBalancer) error {
	req := slb.CreateCreateLoadBalancerRequest()
	setRequest(req, mdl)
	req.ClientToken = utils.GetUUID()
	resp, err := p.auth.SLB.CreateLoadBalancer(req)
	if err != nil {
		return util.FormatErrorMessage(err)
	}
	mdl.LoadBalancerAttribute.LoadBalancerId = resp.LoadBalancerId
	mdl.LoadBalancerAttribute.Address = resp.Address
	return nil

}

func (p SLBProvider) DescribeLoadBalancer(ctx context.Context, mdl *model.LoadBalancer) error {
	req := slb.CreateDescribeLoadBalancerAttributeRequest()
	req.LoadBalancerId = mdl.LoadBalancerAttribute.LoadBalancerId
	resp, err := p.auth.SLB.DescribeLoadBalancerAttribute(req)
	if err != nil {
		return util.FormatErrorMessage(err)
	}
	klog.V(5).Infof("RequestId: %s, API: %s, lbId: %s", resp.RequestId, "DescribeLoadBalancer", req.LoadBalancerId)
	loadResponse(resp, mdl)
	return nil
}

func (p SLBProvider) DeleteLoadBalancer(ctx context.Context, mdl *model.LoadBalancer) error {
	req := slb.CreateDeleteLoadBalancerRequest()
	req.LoadBalancerId = mdl.LoadBalancerAttribute.LoadBalancerId
	_, err := p.auth.SLB.DeleteLoadBalancer(req)
	return util.FormatErrorMessage(err)
}

func (p SLBProvider) SetLoadBalancerDeleteProtection(ctx context.Context, lbId string, flag string) error {
	req := slb.CreateSetLoadBalancerDeleteProtectionRequest()
	req.LoadBalancerId = lbId
	req.DeleteProtection = flag
	_, err := p.auth.SLB.SetLoadBalancerDeleteProtection(req)
	return util.FormatErrorMessage(err)
}

func (p SLBProvider) ModifyLoadBalancerInstanceSpec(ctx context.Context, lbId string, spec string) error {
	req := slb.CreateModifyLoadBalancerInstanceSpecRequest()
	req.LoadBalancerId = lbId
	req.LoadBalancerSpec = spec
	_, err := p.auth.SLB.ModifyLoadBalancerInstanceSpec(req)
	return util.FormatErrorMessage(err)
}

func (p SLBProvider) SetLoadBalancerName(ctx context.Context, lbId string, name string) error {
	req := slb.CreateSetLoadBalancerNameRequest()
	req.LoadBalancerId = lbId
	req.LoadBalancerName = name
	_, err := p.auth.SLB.SetLoadBalancerName(req)
	return util.FormatErrorMessage(err)
}

func (p SLBProvider) ModifyLoadBalancerInternetSpec(ctx context.Context, lbId string, chargeType string, bandwidth int) error {
	req := slb.CreateModifyLoadBalancerInternetSpecRequest()
	req.LoadBalancerId = lbId
	req.InternetChargeType = chargeType
	req.Bandwidth = requests.NewInteger(bandwidth)
	_, err := p.auth.SLB.ModifyLoadBalancerInternetSpec(req)
	return util.FormatErrorMessage(err)
}

func (p SLBProvider) SetLoadBalancerModificationProtection(ctx context.Context, lbId string, flag string) error {
	req := slb.CreateSetLoadBalancerModificationProtectionRequest()
	req.LoadBalancerId = lbId
	req.ModificationProtectionStatus = flag
	if flag == string(model.OnFlag) {
		req.ModificationProtectionReason = model.ModificationProtectionReason
	}
	_, err := p.auth.SLB.SetLoadBalancerModificationProtection(req)
	return util.FormatErrorMessage(err)
}

func (p SLBProvider) AddTags(ctx context.Context, lbId string, tags string) error {
	req := slb.CreateAddTagsRequest()
	req.LoadBalancerId = lbId
	req.Tags = tags
	_, err := p.auth.SLB.AddTags(req)
	return util.FormatErrorMessage(err)
}

func (p SLBProvider) DescribeTags(ctx context.Context, lbId string) ([]model.Tag, error) {
	req := slb.CreateDescribeTagsRequest()
	req.LoadBalancerId = lbId
	resp, err := p.auth.SLB.DescribeTags(req)
	if err != nil {
		return nil, util.FormatErrorMessage(err)
	}
	var tags []model.Tag
	for _, tag := range resp.TagSets.TagSet {
		tags = append(tags, model.Tag{
			TagValue: tag.TagValue,
			TagKey:   tag.TagKey,
		})
	}
	return tags, nil
}

func setRequest(request *slb.CreateLoadBalancerRequest, mdl *model.LoadBalancer) {
	if mdl.LoadBalancerAttribute.AddressType != "" {
		request.AddressType = string(mdl.LoadBalancerAttribute.AddressType)
	}
	if mdl.LoadBalancerAttribute.InternetChargeType != "" {
		request.InternetChargeType = string(mdl.LoadBalancerAttribute.InternetChargeType)
	}
	if mdl.LoadBalancerAttribute.Bandwidth != 0 {
		request.Bandwidth = requests.NewInteger(mdl.LoadBalancerAttribute.Bandwidth)
	}
	if mdl.LoadBalancerAttribute.LoadBalancerName != "" {
		request.LoadBalancerName = mdl.LoadBalancerAttribute.LoadBalancerName
	}
	if mdl.LoadBalancerAttribute.VpcId != "" {
		request.VpcId = mdl.LoadBalancerAttribute.VpcId
	}
	if mdl.LoadBalancerAttribute.VSwitchId != "" {
		request.VSwitchId = mdl.LoadBalancerAttribute.VSwitchId
	}
	if mdl.LoadBalancerAttribute.MasterZoneId != "" {
		request.MasterZoneId = mdl.LoadBalancerAttribute.MasterZoneId
	}
	if mdl.LoadBalancerAttribute.SlaveZoneId != "" {
		request.SlaveZoneId = mdl.LoadBalancerAttribute.SlaveZoneId
	}
	if mdl.LoadBalancerAttribute.LoadBalancerSpec != "" {
		request.LoadBalancerSpec = string(mdl.LoadBalancerAttribute.LoadBalancerSpec)
	}
	if mdl.LoadBalancerAttribute.ResourceGroupId != "" {
		request.ResourceGroupId = mdl.LoadBalancerAttribute.ResourceGroupId
	}
	if mdl.LoadBalancerAttribute.AddressIPVersion != "" {
		request.AddressIPVersion = string(mdl.LoadBalancerAttribute.AddressIPVersion)
	}
	if mdl.LoadBalancerAttribute.DeleteProtection != "" {
		request.DeleteProtection = string(mdl.LoadBalancerAttribute.DeleteProtection)
	}
	if mdl.LoadBalancerAttribute.ModificationProtectionStatus != "" {
		request.ModificationProtectionStatus = string(mdl.LoadBalancerAttribute.ModificationProtectionStatus)
		request.ModificationProtectionReason = mdl.LoadBalancerAttribute.ModificationProtectionReason
	}
}

func loadResponse(resp *slb.DescribeLoadBalancerAttributeResponse, lb *model.LoadBalancer) {
	lb.LoadBalancerAttribute.LoadBalancerId = resp.LoadBalancerId
	lb.LoadBalancerAttribute.LoadBalancerName = resp.LoadBalancerName
	lb.LoadBalancerAttribute.Address = resp.Address
	lb.LoadBalancerAttribute.AddressType = model.AddressType(resp.AddressType)
	lb.LoadBalancerAttribute.AddressIPVersion = model.AddressIPVersionType(resp.AddressIPVersion)
	lb.LoadBalancerAttribute.NetworkType = resp.NetworkType
	lb.LoadBalancerAttribute.VpcId = resp.VpcId
	lb.LoadBalancerAttribute.VSwitchId = resp.VSwitchId
	lb.LoadBalancerAttribute.Bandwidth = resp.Bandwidth
	lb.LoadBalancerAttribute.MasterZoneId = resp.MasterZoneId
	lb.LoadBalancerAttribute.SlaveZoneId = resp.SlaveZoneId
	lb.LoadBalancerAttribute.DeleteProtection = model.FlagType(resp.DeleteProtection)
	lb.LoadBalancerAttribute.InternetChargeType = model.InternetChargeType(resp.InternetChargeType)
	lb.LoadBalancerAttribute.LoadBalancerSpec = model.LoadBalancerSpecType(resp.LoadBalancerSpec)
	lb.LoadBalancerAttribute.ModificationProtectionStatus = model.ModificationProtectionType(resp.ModificationProtectionStatus)
	lb.LoadBalancerAttribute.ResourceGroupId = resp.ResourceGroupId
}
