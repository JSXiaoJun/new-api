package constant

// MaxTaskDurationSeconds caps user-supplied task/video duration. Duration is
// used as a billing multiplier, so an unbounded value could overflow quota
// calculation. It lives here so setting and plugin packages can share it
// without importing relay/common.
const MaxTaskDurationSeconds = 3600
