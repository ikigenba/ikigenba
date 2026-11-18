package awssdk

import (
	"maps"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

func hasForbiddenTags(tags cloud.Tags) bool {
	_, space := tags["Space"]
	_, domain := tags["Domain"]
	return space || domain
}

func validFilter(tags cloud.Tags) bool {
	return len(tags) != 0 && !hasForbiddenTags(tags)
}

func validKind(tags cloud.Tags, kind string) bool {
	return tags[cloud.TagKind] == kind && !hasForbiddenTags(tags)
}

func hasKind(tags cloud.Tags) bool {
	_, has := tags[cloud.TagKind]
	return has
}

func validLaunch(l cloud.Launch) bool {
	family, _, found := strings.Cut(l.Type, ".")
	if !found {
		return false
	}
	switch family {
	case "t4g", "m7g", "c7g", "r7g":
		return validKind(l.Tags, cloud.KindHost) && !hasKind(l.InstanceTags) && !hasForbiddenTags(l.InstanceTags)
	default:
		return false
	}
}

func toTags(tags cloud.Tags) []types.Tag {
	result := make([]types.Tag, 0, len(tags))
	for _, key := range slices.Sorted(maps.Keys(tags)) {
		result = append(result, types.Tag{Key: aws.String(key), Value: aws.String(tags[key])})
	}
	return result
}

func fromTags(tags []types.Tag) cloud.Tags {
	result := make(cloud.Tags, len(tags))
	for _, tag := range tags {
		result[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return result
}

func tagSpec(resource types.ResourceType, tags cloud.Tags) types.TagSpecification {
	return types.TagSpecification{ResourceType: resource, Tags: toTags(tags)}
}

// EC2 interprets wildcard syntax in filter values. Escape it for literal equality.
var literalTagValue = strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?")

func tagFilters(tags cloud.Tags) []types.Filter {
	filters := make([]types.Filter, 0, len(tags))
	for _, key := range slices.Sorted(maps.Keys(tags)) {
		filters = append(filters, types.Filter{Name: aws.String("tag:" + key), Values: []string{literalTagValue.Replace(tags[key])}})
	}
	return filters
}
