# Developer Sandbox UI E2E Tests
The UI E2E tests are executed against the Developer Sandbox UI running in OpenShift.

*Prerequisites*:

1. You need a OCP cluster 
    - ROSA cluster from ClusterBot will not work since we are not able to modify the OAuth configuration of ROSA clusters created by the ClusterBot.
2. Ensure you are using Node.js version 22
    - to easily manage it, you can run `nvm use 22`
3. Ensure you have `yarn` installed
4. Make sure you can log in at https://sso.devsandbox.dev/auth/realms/sandbox-dev/account using your SSO_USERNAME and SSO_PASSWORD
5. Make sure you do not have any toolchain resources deployed on your cluster

### Running UI E2E Tests locally
`make test-ui-e2e SSO_USERNAME=${SSO_USERNAME} SSO_PASSWORD=${SSO_PASSWORD}`

If you want to run and test the Developer Sandbox UI from your local rhdh-plugins repo, run `make test-ui-e2e-local SSO_USERNAME=${SSO_USERNAME} SSO_PASSWORD=${SSO_PASSWORD}`

For now, the UI E2E tests are running only through the Firefox browser.

### Running UI E2E Tests in Container

`make test-sandbox-ui-in-container SSO_USERNAME=<SSO_USERNAME> SSO_PASSWORD=<SSO_PASSWORD>`

### Running UI E2E Tests against dev/stage/prod
If you want to run the UI E2E tests against dev/stage/prod, please follow the next steps:
- have a new Red Hat account or an account that was deactivated
- fill `testsupport/sandbox-ui/.env`, note that to run the UI E2E tests against dev/stage/prod, you need to set the ENVIRONMENT to dev.
```
SSO_USERNAME=<your-username>
SSO_PASSWORD=<your-password>
BASE_URL=https://sandbox.redhat.com/
ENVIRONMENT=dev
BROWSER=firefox
```
- run `go test "./test/e2e/sandbox-ui" -v -timeout=10m -failfast`

`make test-sandbox-ui-in-container SSO_USERNAME=<SSO_USERNAME> SSO_PASSWORD=<SSO_PASSWORD>`

### Deploy Developer Sandbox UI in E2E mode
`make deploy-sandbox-ui HOST_NS=<HOST_NS>`

Please note that OCP cluster does not have a valid CA, so when accessing the Developer Sandbox UI, you need to:
 
- accept to proceed unsafely

![private-connection](https://github.com/user-attachments/assets/5b35a65f-6703-42cf-a165-b7326fd4faab)

- access `<registration-service-route>/api/v1/signup` to tell your browser that the registration service route can be accessed

![registration-service](https://github.com/user-attachments/assets/6c2f7446-1de2-4701-ace7-2d6796f49eeb)

### Clean Developer Sandbox UI
`make clean-sandbox-ui HOST_NS=<HOST_NS> SSO_USERNAME=<SSO_USERNAME>`