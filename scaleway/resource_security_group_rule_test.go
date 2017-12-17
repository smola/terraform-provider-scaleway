package scaleway

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
	api "github.com/nicolai86/scaleway-sdk"
)

func TestAccScalewaySecurityGroupRule_Basic(t *testing.T) {
	var group api.SecurityGroups

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckScalewaySecurityGroupRuleDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccCheckScalewaySecurityGroupRuleConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckScalewaySecurityGroupsExists("scaleway_security_group.base", &group),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.http", "action", "accept"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.http", "direction", "inbound"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.http", "ip_range", "0.0.0.0/0"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.http", "protocol", "TCP"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.http", "port", "80"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.https", "action", "accept"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.https", "direction", "inbound"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.https", "ip_range", "0.0.0.0/0"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.https", "protocol", "TCP"),
					resource.TestCheckResourceAttr("scaleway_security_group_rule.https", "port", "443"),
					testAccCheckScalewaySecurityGroupRuleExists("scaleway_security_group_rule.http", &group),
					testAccCheckScalewaySecurityGroupRuleAttributes("scaleway_security_group_rule.http", &group),
				),
			},
		},
	})
}

func TestAccScalewaySecurityGroupRule_Count(t *testing.T) {
	var group api.SecurityGroups

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckScalewaySecurityGroupRuleDestroy,
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccCheckScalewaySecurityGroupRuleVariablesConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckScalewaySecurityGroupsExists("scaleway_security_group.base", &group),
					testAccCheckScalewaySecurityGroupRuleExists("scaleway_security_group_rule.rule.0", &group),
					testAccCheckScalewaySecurityGroupRuleExists("scaleway_security_group_rule.rule.1", &group),
				),
			},
		},
	})
}

func testAccCheckScalewaySecurityGroupsExists(n string, group *api.SecurityGroups) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Security Group Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No Security Group is set")
		}

		conn := testAccProvider.Meta().(*Client).scaleway
		var (
			resp *api.GetSecurityGroup
			err  error
		)
		if err = retry(func() error {
			resp, err = conn.GetASecurityGroup(rs.Primary.ID)
			return err
		}); err != nil {
			return err
		}

		if resp.SecurityGroups.ID == rs.Primary.ID {
			*group = resp.SecurityGroups
			return nil
		}

		return fmt.Errorf("Security Group not found")
	}
}

func testAccCheckScalewaySecurityGroupRuleDestroy(s *terraform.State) error {
	client := testAccProvider.Meta().(*Client).scaleway

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "scaleway" {
			continue
		}

		var (
			groups *api.GetSecurityGroups
			err    error
		)
		if err = retry(func() error {
			groups, err = client.GetSecurityGroups()
			return err
		}); err != nil {
			return err
		}

		all_err := true
		for _, group := range groups.SecurityGroups {
			err := retry(func() error {
				_, err := client.GetASecurityGroupRule(group.ID, rs.Primary.ID)
				return err
			})
			all_err = all_err && err != nil
		}

		if !all_err {
			return fmt.Errorf("Security Group still exists")
		}
	}

	return nil
}

func testAccCheckScalewaySecurityGroupRuleAttributes(n string, group *api.SecurityGroups) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Unknown resource: %s", n)
		}

		client := testAccProvider.Meta().(*Client).scaleway
		var (
			rule *api.GetSecurityGroupRule
			err  error
		)
		if err = retry(func() error {
			rule, err = client.GetASecurityGroupRule(group.ID, rs.Primary.ID)
			return err
		}); err != nil {
			return err
		}

		if rule.Rules.Action != "accept" {
			return fmt.Errorf("Wrong rule action")
		}
		if rule.Rules.Direction != "inbound" {
			return fmt.Errorf("wrong rule direction")
		}
		if rule.Rules.IPRange != "0.0.0.0/0" {
			return fmt.Errorf("wrong rule IP Range")
		}
		if rule.Rules.Protocol != "TCP" {
			return fmt.Errorf("wrong rule protocol")
		}
		if rule.Rules.DestPortFrom != 80 {
			return fmt.Errorf("Wrong port")
		}

		return nil
	}
}

func testAccCheckScalewaySecurityGroupRuleExists(n string, group *api.SecurityGroups) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]

		if !ok {
			return fmt.Errorf("Security Group Rule Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No Security Group Rule ID is set")
		}

		client := testAccProvider.Meta().(*Client).scaleway
		var (
			rule *api.GetSecurityGroupRule
			err  error
		)
		if err = retry(func() error {
			rule, err = client.GetASecurityGroupRule(group.ID, rs.Primary.ID)
			return err
		}); err != nil {
			return err
		}

		if rule.Rules.ID != rs.Primary.ID {
			return fmt.Errorf("Record not found")
		}

		return nil
	}
}

var testAccCheckScalewaySecurityGroupRuleConfig = `
resource "scaleway_security_group" "base" {
  name = "public"
  description = "public gateway"
}

resource "scaleway_security_group_rule" "http" {
  security_group = "${scaleway_security_group.base.id}"

  action = "accept"
  direction = "inbound"
  ip_range = "0.0.0.0/0"
  protocol = "TCP"
  port = 80
}

resource "scaleway_security_group_rule" "https" {
  security_group = "${scaleway_security_group.base.id}"

  action = "accept"
  direction = "inbound"
  ip_range = "0.0.0.0/0"
  protocol = "TCP"
  port = 443
}
`

var testAccCheckScalewaySecurityGroupRuleVariablesConfig = `
variable "trusted_ips" {
    type        = "list"
    default = ["1.1.1.1", "2.2.2.2/31"]
}

resource "scaleway_security_group" "base" {
    name        = "sg"
    description = "sg rules"
}

resource "scaleway_security_group_rule" "rule" {
    count = "${length(var.trusted_ips)}"

    security_group = "${scaleway_security_group.base.id}"

    action    = "accept"
    direction = "inbound"
    ip_range  = "${element(var.trusted_ips, count.index)}"
    protocol  = "TCP"
    port      = "22"
}`
