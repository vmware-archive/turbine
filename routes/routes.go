package routes

import "github.com/tedsuo/rata"

const (
	ExecuteBuild     = "ExecuteBuild"
	DeleteBuild      = "DeleteBuild"
	AbortBuild       = "AbortBuild"
	HijackBuild      = "HijackBuild"
	GetBuildEvents   = "GetBuildEvents"
	CheckInput       = "CheckInput"
	CheckInputStream = "CheckInputStream"
)

var Routes = rata.Routes{
	{Path: "/builds", Method: "POST", Name: ExecuteBuild},
	{Path: "/builds/:guid", Method: "DELETE", Name: DeleteBuild},
	{Path: "/builds/:guid/abort", Method: "POST", Name: AbortBuild},
	{Path: "/builds/:guid/hijack", Method: "POST", Name: HijackBuild},
	{Path: "/builds/:guid/events", Method: "GET", Name: GetBuildEvents},
	{Path: "/checks", Method: "POST", Name: CheckInput},
	{Path: "/checks/stream", Method: "GET", Name: CheckInputStream},
}
