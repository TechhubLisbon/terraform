package heroku

import (
	"fmt"
	"log"

	"github.com/bgentry/heroku-go"
	"github.com/hashicorp/terraform/flatmap"
	"github.com/hashicorp/terraform/helper/config"
	"github.com/hashicorp/terraform/helper/diff"
	"github.com/hashicorp/terraform/helper/multierror"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
)

// type application is used to store all the details of a heroku app
type application struct {
	Id string // Id of the resource

	App    *heroku.App       // The heroku application
	Client *heroku.Client    // Client to interact with the heroku API
	Vars   map[string]string // The vars on the application
}

// Updates the application to have the latest from remote
func (a *application) Update() error {
	var errs []error
	var err error

	a.App, err = a.Client.AppInfo(a.Id)
	if err != nil {
		errs = append(errs, err)
	}

	a.Vars, err = retrieve_config_vars(a.Id, a.Client)
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return &multierror.Error{Errors: errs}
	}

	return nil
}

func resourceHerokuApp() *schema.Resource {
	return &schema.Resource{
		Create: resourceHerokuAppCreate,
		Read:   resourceHerokuAppRead,
		Update: resourceHerokuAppUpdate,
		Delete: resourceHerokuAppDelete,

		Schema: map[string]*schema.Schema{
			"name": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"region": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"stack": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},

			"config_vars": &schema.Schema{
				Type:     schema.TypeList,
				Optional: true,
				Elem: &schema.Schema{
					// TODO: make map
					Type: schema.TypeString,
				},
			},

			"git_url": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"web_url": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},

			"heroku_hostname": &schema.Schema{
				Type:     schema.TypeString,
				Computed: true,
			},
		},
	}
}

func resourceHerokuAppCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Client)

	// Build up our creation options
	opts := heroku.AppCreateOpts{}

	if v := d.Get("name"); v != nil {
		vs := v.(string)
		opts.Name = &vs
	}
	if v := d.Get("region"); v != nil {
		vs := v.(string)
		opts.Region = &vs
	}
	if v := d.Get("stack"); v != nil {
		vs := v.(string)
		opts.Stack = &vs
	}

	log.Printf("[DEBUG] App create configuration: %#v", opts)

	a, err := client.AppCreate(&opts)
	if err != nil {
		return err
	}

	d.SetId(a.Name)
	log.Printf("[INFO] App ID: %s", d.Id())

	if v := d.Get("config_vars"); v != nil {
		err = update_config_vars(d.Id(), v.([]interface{}), client)
		if err != nil {
			return err
		}
	}

	return resourceHerokuAppRead(d, meta)
}

func resourceHerokuAppRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Client)
	app, err := resource_heroku_app_retrieve(d.Id(), client)
	if err != nil {
		return err
	}

	d.Set("name", app.App.Name)
	d.Set("stack", app.App.Stack.Name)
	d.Set("region", app.App.Region.Name)
	d.Set("git_url", app.App.GitURL)
	d.Set("web_url", app.App.WebURL)
	d.Set("config_vars", []map[string]string{app.Vars})

	// We know that the hostname on heroku will be the name+herokuapp.com
	// You need this to do things like create DNS CNAME records
	d.Set("heroku_hostname", fmt.Sprintf("%s.herokuapp.com", app.App.Name))

	return nil
}

func resourceHerokuAppUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Client)

	// If name changed
	// TODO
	/*
		if attr, ok := d.Attributes["name"]; ok {
			opts := heroku.AppUpdateOpts{
				Name: &attr.New,
			}

			renamedApp, err := client.AppUpdate(rs.ID, &opts)

			if err != nil {
				return s, err
			}

			// Store the new ID
			rs.ID = renamedApp.Name
		}
	*/

	v := d.Get("config_vars")
	if v == nil {
		v = []interface{}{}
	}

	err := update_config_vars(d.Id(), v.([]interface{}), client)
	if err != nil {
		return err
	}

	return resourceHerokuAppRead(d, meta)
}

func resourceHerokuAppDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*heroku.Client)

	log.Printf("[INFO] Deleting App: %s", d.Id())
	err := client.AppDelete(d.Id())
	if err != nil {
		return fmt.Errorf("Error deleting App: %s", err)
	}

	d.SetId("")
	return nil
}

func resource_heroku_app_diff(
	s *terraform.ResourceState,
	c *terraform.ResourceConfig,
	meta interface{}) (*terraform.ResourceDiff, error) {

	b := &diff.ResourceBuilder{
		Attrs: map[string]diff.AttrType{
			"name":        diff.AttrTypeUpdate,
			"region":      diff.AttrTypeUpdate,
			"stack":       diff.AttrTypeCreate,
			"config_vars": diff.AttrTypeUpdate,
		},

		ComputedAttrs: []string{
			"name",
			"region",
			"stack",
			"git_url",
			"web_url",
			"id",
			"config_vars",
		},

		ComputedAttrsUpdate: []string{
			"heroku_hostname",
		},
	}

	return b.Diff(s, c)
}

func resource_heroku_app_update_state(
	s *terraform.ResourceState,
	app *application) (*terraform.ResourceState, error) {

	s.Attributes["name"] = app.App.Name
	s.Attributes["stack"] = app.App.Stack.Name
	s.Attributes["region"] = app.App.Region.Name
	s.Attributes["git_url"] = app.App.GitURL
	s.Attributes["web_url"] = app.App.WebURL

	// We know that the hostname on heroku will be the name+herokuapp.com
	// You need this to do things like create DNS CNAME records
	s.Attributes["heroku_hostname"] = fmt.Sprintf("%s.herokuapp.com", app.App.Name)

	toFlatten := make(map[string]interface{})

	if len(app.Vars) > 0 {
		toFlatten["config_vars"] = []map[string]string{app.Vars}
	}

	for k, v := range flatmap.Flatten(toFlatten) {
		s.Attributes[k] = v
	}

	return s, nil
}

func resource_heroku_app_retrieve(id string, client *heroku.Client) (*application, error) {
	app := application{Id: id, Client: client}

	err := app.Update()

	if err != nil {
		return nil, fmt.Errorf("Error retrieving app: %s", err)
	}

	return &app, nil
}

func resource_heroku_app_validation() *config.Validator {
	return &config.Validator{
		Required: []string{},
		Optional: []string{
			"name",
			"region",
			"stack",
			"config_vars.*",
		},
	}
}

func retrieve_config_vars(id string, client *heroku.Client) (map[string]string, error) {
	vars, err := client.ConfigVarInfo(id)

	if err != nil {
		return nil, err
	}

	return vars, nil
}

// Updates the config vars for from an expanded configuration.
func update_config_vars(id string, vs []interface{}, client *heroku.Client) error {
	vars := make(map[string]*string)

	for _, v := range vs {
		for k, v := range v.(map[string]interface{}) {
			val := v.(string)
			vars[k] = &val
		}
	}

	log.Printf("[INFO] Updating config vars: *%#v", vars)
	if _, err := client.ConfigVarUpdate(id, vars); err != nil {
		return fmt.Errorf("Error updating config vars: %s", err)
	}

	return nil
}
