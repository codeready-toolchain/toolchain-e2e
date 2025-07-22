# Developer Sandbox UI E2E Tests
The e2e tests are executed against the Developer Sandbox UI running in OpenShift.

*Prerequisites*:

1. You need a OCP cluster 
    - ROSA cluster from ClusterBot will not work since we are not able to modify the OAuth configuration of ROSA clusters created by the ClusterBot.
2. Ensure you are using Node.js version 22
    - to easily manage it, you can run `nvm use 22`
3. Ensure you have `yarn` installed
4. Make sure you can log in at https://sso.devsandbox.dev/auth/realms/sandbox-dev/account using your SSO_USERNAME and SSO_PASSWORD
5. Make sure you do not have any toolchain resources deployed on your cluster

### Running E2E Tests locally
`make test-ui-e2e SSO_USERNAME=${SSO_USERNAME} SSO_PASSWORD=${SSO_PASSWORD}`

If you want to run and test the Developer Sandbox UI from your local rhdh-plugins repo, run `make test-ui-e2e-local SSO_USERNAME=${SSO_USERNAME} SSO_PASSWORD=${SSO_PASSWORD}`

For now, the e2e tests are running only through firefox browser.

### Deploy Developer Sandbox UI in E2E mode
`make deploy-sandbox-ui HOST_NS=<HOST_NS>`

Please note that OCP cluster does not have a valid CA, so when accessing the Developer Sandbox UI, you need to:
 
- accept to proceed unsafely

![private-connection](https://github.com/user-attachments/assets/5b35a65f-6703-42cf-a165-b7326fd4faab)

- access `<registration-service-route>/api/v1/signup` to tell your browser that the registration service route can be accessed

![registration-service](https://github.com/user-attachments/assets/6c2f7446-1de2-4701-ace7-2d6796f49eeb)

### Running E2E Tests in Container
Please, do not forget that you need to have the toolchain resources deployed before running:

`make test-sandbox-ui-in-container SSO_USERNAME=<SSO_USERNAME> SSO_PASSWORD=<SSO_PASSWORD>`

### Clean Developer Sandbox UI
`make clean-sandbox-ui HOST_NS=<HOST_NS> SSO_USERNAME=<SSO_USERNAME>`