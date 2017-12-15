// @APIVersion 1.0.0
// @Title beego Test API
// @Description beego has a very cool tools to autogenerate documents for your API
// @Contact astaxie@gmail.com
// @TermsOfServiceUrl http://beego.me/
// @License Apache 2.0
// @LicenseUrl http://www.apache.org/licenses/LICENSE-2.0.html
package routers

import (
	//"vil_tools/controllers"
	"vil_tools/controllers/tools"
	"vil_tools/controllers/users"

	"github.com/astaxie/beego"
)

func init() {
	ns := beego.NewNamespace("/v1",
		beego.NSNamespace("/user",
			beego.NSInclude(
				&userControllers.UserControllers{},
			),
		),
		beego.NSNamespace("/tools",
			beego.NSInclude(
				&toolControllers.ToolControllers{},
			),
		),
	)
	beego.AddNamespace(ns)
}
