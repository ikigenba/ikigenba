# Permit tests of daemon signal and error exits

During the accounts build from `0a129295`, D14 R-KJN3-UYRO required the binary to cancel on SIGTERM and SIGINT in its daemon and stdio modes and translate errors from both helpers into process exit statuses. Accounts AGENTS.md permits exactly the four child cases in D14 R-KKV0-8QID. Those cases exercise mode selection and expected success or usage output. They end without signals and without helper failure, so they cannot distinguish incorrect signal handling or helper error mapping.

Permit targeted offline child cases for those branches, or move the unobservable process behavior to design guidance and retain the tagged four-case contract.
