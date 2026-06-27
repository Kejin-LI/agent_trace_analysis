PUSH_BRANCH ?= ""
PULL_BRANCH ?= "master"

add-test-idl:
		git subtree add --prefix=auto-tests/idls git@code.byted.org:inf/metrics_autotest_idl.git ${PULL_BRANCH} --squash

pull-test-idl:
		git subtree pull --prefix=auto-tests/idls git@code.byted.org:inf/metrics_autotest_idl.git ${PULL_BRANCH} --squash

push-test-idl:
		git subtree push --prefix=auto-tests/idls git@code.byted.org:inf/metrics_autotest_idl.git ${PUSH_BRANCH}

build-test-idl:
		cd auto-tests && kitex -module code.byted.org/gopkg/metrics/auto-tests -service inf.metrics.autotest_executor_go idls/metrics.thrift