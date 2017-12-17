package scaleway

import (
	"fmt"
	"log"

	"github.com/hashicorp/terraform/helper/schema"
	api "github.com/nicolai86/scaleway-sdk"
)

var commercialServerTypes []string

func resourceScalewayServer() *schema.Resource {
	return &schema.Resource{
		Create: resourceScalewayServerCreate,
		Read:   resourceScalewayServerRead,
		Update: resourceScalewayServerUpdate,
		Delete: resourceScalewayServerDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The name of the server",
			},
			"image": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The base image of the server",
			},
			"type": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "The instance type of the server",
				ValidateFunc: validateServerType,
			},
			"bootscript": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The boot configuration of the server",
			},
			"tags": {
				Type: schema.TypeList,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional:    true,
				Description: "The tags associated with the server",
			},
			"security_group": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "The security group the server is attached to",
			},
			"volume": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"size_in_gb": {
							Type:     schema.TypeInt,
							Required: true,
							ValidateFunc: func(v interface{}, k string) (ws []string, errors []error) {
								value := v.(int)
								if value > 150 {
									errors = append(errors, fmt.Errorf("%q needs to be less than 150", k))
								}
								return
							},
						},
						"type": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validateVolumeType,
						},
						"volume_id": {
							Type:     schema.TypeString,
							Computed: true,
						},
					},
				},
				Description: "Volumes attached to the server on creation",
			},
			"enable_ipv6": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "determines if IPv6 is enabled for the server",
			},
			"dynamic_ip_required": {
				Type:        schema.TypeBool,
				Optional:    true,
				Description: "determines if a public IP address should be allocated for the server",
			},
			"private_ip": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "the Scaleway internal IP address of the server",
			},
			"public_ip": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "the public IPv4 address of the server",
			},
			"public_ipv6": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "the public IPv6 address of the server, if enabled",
			},
			"state": {
				Type:        schema.TypeString,
				Optional:    true,
				Computed:    true,
				Description: "the server state (running, stopped)",
			},
			"state_detail": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "scaleway description of the server state",
			},
			"user_data": {
				Type:        schema.TypeMap,
				Optional:    true,
				Elem:        schema.TypeString,
				Description: "user data key-value pairs associated to this server",
			},
		},
	}
}

func attachIP(scaleway *api.API, serverID, IPAddress string) error {
	ips, err := scaleway.GetIPS()
	if err != nil {
		return err
	}
	for _, ip := range ips.IPS {
		if ip.Address == IPAddress {
			log.Printf("[DEBUG] Attaching IP %q to server %q\n", ip.ID, serverID)
			return scaleway.AttachIP(ip.ID, serverID)
		}
	}
	return fmt.Errorf("Failed to find IP with ip %q to attach", IPAddress)
}

func resourceScalewayServerCreate(d *schema.ResourceData, m interface{}) error {
	scaleway := m.(*Client).scaleway

	mu.Lock()
	defer mu.Unlock()

	image := d.Get("image").(string)
	var server = api.ServerDefinition{
		Name:          d.Get("name").(string),
		Image:         String(image),
		Organization:  scaleway.Organization,
		EnableIPV6:    d.Get("enable_ipv6").(bool),
		SecurityGroup: d.Get("security_group").(string),
	}

	server.DynamicIPRequired = Bool(d.Get("dynamic_ip_required").(bool))
	server.CommercialType = d.Get("type").(string)

	if bootscript, ok := d.GetOk("bootscript"); ok {
		server.Bootscript = String(bootscript.(string))
	}

	if vs, ok := d.GetOk("volume"); ok {
		server.Volumes = make(map[string]string)

		volumes := vs.([]interface{})
		for i, v := range volumes {
			volume := v.(map[string]interface{})
			sizeInGB := uint64(volume["size_in_gb"].(int))

			if sizeInGB > 0 {
				var (
					volumeID string
					err      error
				)
				if err = retry(func() error {
					volumeID, err = scaleway.PostVolume(api.VolumeDefinition{
						Size: sizeInGB * gb,
						Type: volume["type"].(string),
						Name: fmt.Sprintf("%s-%d", server.Name, sizeInGB),
					})
					return err
				}); err != nil {
					return err
				}
				volume["volume_id"] = volumeID
				server.Volumes[fmt.Sprintf("%d", i+1)] = volumeID
			}
			volumes[i] = volume
		}
		d.Set("volume", volumes)
	}

	if raw, ok := d.GetOk("tags"); ok {
		for _, tag := range raw.([]interface{}) {
			server.Tags = append(server.Tags, tag.(string))
		}
	}

	var (
		id  string
		err error
	)
	if err := retry(func() error {
		id, err = scaleway.PostServer(server)
		return err
	}); err != nil {
		return err
	}

	d.SetId(id)
	if ud, ok := d.GetOk("user_data"); ok {
		for k, v := range ud.(map[string]interface{}) {
			sv := []byte(v.(string))
			if err := scaleway.PatchUserdata(id, k, sv, false); err != nil {
				return err
			}
		}
	}

	if d.Get("state").(string) != "stopped" {
		err = poweronServer(scaleway, id)
		if v, ok := d.GetOk("public_ip"); ok {
			if err := attachIP(scaleway, d.Id(), v.(string)); err != nil {
				return err
			}
		}
	}

	if err != nil {
		return err
	}

	return resourceScalewayServerRead(d, m)
}

func resourceScalewayServerRead(d *schema.ResourceData, m interface{}) error {
	scaleway := m.(*Client).scaleway
	var (
		server *api.Server
		err    error
	)
	if err := retry(func() error {
		server, err = scaleway.GetServer(d.Id())
		return err
	}); err != nil {
		if serr, ok := err.(api.APIError); ok {
			log.Printf("[DEBUG] Error reading server: %q\n", serr.APIMessage)

			if serr.StatusCode == 404 {
				d.SetId("")
				return nil
			}
		}

		return err
	}

	d.Set("name", server.Name)
	d.Set("image", server.Image.Identifier)
	d.Set("type", server.CommercialType)
	d.Set("enable_ipv6", server.EnableIPV6)
	d.Set("private_ip", server.PrivateIP)
	d.Set("public_ip", server.PublicAddress.IP)

	if server.EnableIPV6 {
		d.Set("public_ipv6", server.IPV6.Address)
	}

	d.Set("state", server.State)
	d.Set("state_detail", server.StateDetail)
	d.Set("tags", server.Tags)

	d.SetConnInfo(map[string]string{
		"type": "ssh",
		"host": server.PublicAddress.IP,
	})

	ud, err := readUserDatas(scaleway, d.Id())
	if err != nil {
		return err
	}

	if len(ud) > 0 {
		d.Set("user_data", ud)
	}

	return nil
}

func resourceScalewayServerUpdate(d *schema.ResourceData, m interface{}) error {
	scaleway := m.(*Client).scaleway

	mu.Lock()
	defer mu.Unlock()

	var req api.ServerPatchDefinition
	if d.HasChange("name") {
		name := d.Get("name").(string)
		req.Name = &name
	}

	if d.HasChange("tags") {
		if raw, ok := d.GetOk("tags"); ok {
			var tags []string
			for _, tag := range raw.([]interface{}) {
				tags = append(tags, tag.(string))
			}
			req.Tags = &tags
		}
	}

	if d.HasChange("enable_ipv6") {
		req.EnableIPV6 = Bool(d.Get("enable_ipv6").(bool))
	}

	if d.HasChange("dynamic_ip_required") {
		req.DynamicIPRequired = Bool(d.Get("dynamic_ip_required").(bool))
	}

	if d.HasChange("security_group") {
		req.SecurityGroup = &api.SecurityGroup{
			Identifier: d.Get("security_group").(string),
		}
	}

	if err := retry(func() error {
		return scaleway.PatchServer(d.Id(), req)
	}); err != nil {
		return fmt.Errorf("Failed patching scaleway server: %q", err)
	}

	if d.HasChange("public_ip") {
		var (
			ips *api.GetIPS
			err error
		)
		if err := retry(func() error {
			ips, err = scaleway.GetIPS()
			return err
		}); err != nil {
			return err
		}
		if v, ok := d.GetOk("public_ip"); ok {
			for _, ip := range ips.IPS {
				if ip.Address == v.(string) {
					log.Printf("[DEBUG] Attaching IP %q to server %q\n", ip.ID, d.Id())
					if err := retry(func() error {
						return scaleway.AttachIP(ip.ID, d.Id())
					}); err != nil {
						return err
					}
					break
				}
			}
		} else {
			for _, ip := range ips.IPS {
				if ip.Server != nil && ip.Server.Identifier == d.Id() {
					log.Printf("[DEBUG] Detaching IP %q to server %q\n", ip.ID, d.Id())
					if err := retry(func() error {
						return scaleway.DetachIP(ip.ID)
					}); err != nil {
						return err
					}
					break
				}
			}
		}
	}

	if d.HasChange("user_data") {
		am, err := readUserDatas(scaleway, d.Id())
		if err != nil {
			return err
		}

		m := make(map[string]interface{})
		if om, ok := d.GetOk("user_data"); ok {
			m = om.(map[string]interface{})
		}

		for ak := range am {
			_, ok := m[ak]
			if !ok {
				if err := scaleway.DeleteUserdata(d.Id(), ak, false); err != nil {
					return err
				}
			}
		}

		for k, v := range m {
			av := am[k]
			if av != v {
				bv := []byte(v.(string))
				if err := scaleway.PatchUserdata(d.Id(), k, bv, false); err != nil {
					return err
				}
			}
		}
	}

	return resourceScalewayServerRead(d, m)
}

func resourceScalewayServerDelete(d *schema.ResourceData, m interface{}) error {
	scaleway := m.(*Client).scaleway

	mu.Lock()
	defer mu.Unlock()

	var (
		s   *api.Server
		err error
	)
	if err := retry(func() error {
		s, err = scaleway.GetServer(d.Id())
		return err
	}); err != nil {
		return err
	}

	if s.State == "stopped" {
		return deleteStoppedServer(scaleway, s)
	}

	err = deleteRunningServer(scaleway, s)

	if err == nil {
		d.SetId("")
	}

	return err
}

func readUserDatas(scaleway *api.API, id string) (
	map[string]interface{}, error) {

	uds, err := scaleway.GetUserdatas(id, false)
	if err != nil {
		return nil, err
	}

	m := make(map[string]interface{}, len(uds.UserData))
	for _, k := range uds.UserData {
		ud, err := scaleway.GetUserdata(id, k, false)
		if err != nil {
			return m, err
		}

		m[k] = ud.String()
	}

	return m, nil
}
