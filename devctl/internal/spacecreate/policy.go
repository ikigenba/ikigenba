package spacecreate

// AssumeRolePolicy is the trust policy for a space's EC2 role.
const AssumeRolePolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ec2.amazonaws.com"}}]}`
