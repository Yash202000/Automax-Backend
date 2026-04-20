# LDAP/Active Directory Integration Guide

This document describes the LDAP/Active Directory integration for Automax Backend.

## Overview

The LDAP integration allows users to authenticate against an LDAP or Active Directory server instead of (or in addition to) the local database authentication. When a user successfully authenticates via LDAP, their account is automatically created or updated in the local database with their LDAP attributes.

## Configuration

Add the following environment variables to your `.env` file:

```env
# LDAP/Active Directory Configuration
LDAP_ENABLED=false
LDAP_URL=ldap://localhost:389
LDAP_BASE_DN=dc=example,dc=com
LDAP_BIND_DN=cn=admin,dc=example,dc=com
LDAP_BIND_PASSWORD=adminpassword
LDAP_USER_SEARCH_BASE=ou=users,dc=example,dc=com
LDAP_USER_SEARCH_FILTER=(sAMAccountName={{username}})
LDAP_GROUP_SEARCH_BASE=ou=groups,dc=example,dc=com
LDAP_GROUP_SEARCH_FILTER=(member={{userDN}})
LDAP_INSECURE_SKIP_VERIFY=true
```

### Configuration Options

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `LDAP_ENABLED` | Enable/disable LDAP authentication | `false` | `true` |
| `LDAP_URL` | LDAP server URL (supports `ldap://` and `ldaps://`) | `ldap://localhost:389` | `ldaps://ad.example.com:636` |
| `LDAP_BASE_DN` | Base DN for LDAP searches | `dc=example,dc=com` | `dc=company,dc=com` |
| `LDAP_BIND_DN` | DN for binding to LDAP (service account) | Empty | `cn=automax,ou=service accounts,dc=example,dc=com` |
| `LDAP_BIND_PASSWORD` | Password for bind DN | Empty | `serviceAccountPassword` |
| `LDAP_USER_SEARCH_BASE` | Base DN for user searches | `ou=users,dc=example,dc=com` | `ou=employees,dc=example,dc=com` |
| `LDAP_USER_SEARCH_FILTER` | Filter template for user search (use `{{username}}` placeholder) | `(sAMAccountName={{username}})` | `(&(objectClass=user)(sAMAccountName={{username}}))` |
| `LDAP_GROUP_SEARCH_BASE` | Base DN for group searches | `ou=groups,dc=example,dc=com` | `ou=security groups,dc=example,dc=com` |
| `LDAP_GROUP_SEARCH_FILTER` | Filter template for group search (use `{{userDN}}` placeholder) | `(member={{userDN}})` | `(&(objectClass=group)(member={{userDN}}))` |
| `LDAP_INSECURE_SKIP_VERIFY` | Skip TLS certificate verification (for LDAPS) | `true` | `false` |

### Active Directory Example

```env
LDAP_ENABLED=true
LDAP_URL=ldaps://ad.company.com:636
LDAP_BASE_DN=dc=company,dc=com
LDAP_BIND_DN=cn=automax service,cn=users,dc=company,dc=com
LDAP_BIND_PASSWORD=SecurePassword123!
LDAP_USER_SEARCH_BASE=cn=users,dc=company,dc=com
LDAP_USER_SEARCH_FILTER=(sAMAccountName={{username}})
LDAP_GROUP_SEARCH_BASE=cn=users,dc=company,dc=com
LDAP_GROUP_SEARCH_FILTER=(member={{userDN}})
LDAP_INSECURE_SKIP_VERIFY=false
```

### OpenLDAP Example

```env
LDAP_ENABLED=true
LDAP_URL=ldap://ldap.company.com:389
LDAP_BASE_DN=dc=company,dc=com
LDAP_BIND_DN=cn=admin,dc=company,dc=com
LDAP_BIND_PASSWORD=adminPassword
LDAP_USER_SEARCH_BASE=ou=people,dc=company,dc=com
LDAP_USER_SEARCH_FILTER=(&(objectClass=inetOrgPerson)(uid={{username}}))
LDAP_GROUP_SEARCH_BASE=ou=groups,dc=company,dc=com
LDAP_GROUP_SEARCH_FILTER=(memberUid={{username}})
LDAP_INSECURE_SKIP_VERIFY=true
```

## API Endpoints

### 1. LDAP Login

**Endpoint:** `POST /api/v1/ldap/login`

**Access:** Public (no authentication required)

**Request Body:**
```json
{
  "username": "john.doe",
  "password": "SecurePassword123!"
}
```

**Response (Success):**
```json
{
  "success": true,
  "data": {
    "user": {
      "id": "uuid-here",
      "email": "john.doe@company.com",
      "username": "john.doe",
      "first_name": "John",
      "last_name": "Doe",
      "phone": "+1234567890",
      "is_active": true,
      "roles": [...],
      "permissions": [...]
    },
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
    "expires_in": 86400,
    "source": "ldap"
  }
}
```

**Response (Invalid Credentials):**
```json
{
  "success": false,
  "error": "LDAP authentication failed: invalid credentials"
}
```

### 2. Test LDAP Connection

**Endpoint:** `POST /api/v1/ldap/test`

**Access:** Authenticated users with `admin:ldap` permission

**Response (Success):**
```json
{
  "success": true,
  "message": "LDAP connection successful"
}
```

**Response (Failure):**
```json
{
  "success": false,
  "error": "LDAP connection test failed: failed to connect to LDAP server: ..."
}
```

### 3. Search LDAP User

**Endpoint:** `POST /api/v1/ldap/search`

**Access:** Authenticated users with `users:view` permission

**Request Body:**
```json
{
  "username": "john.doe"
}
```

**Response (User Found):**
```json
{
  "success": true,
  "data": {
    "found": true,
    "user": {
      "dn": "CN=John Doe,CN=Users,DC=company,DC=com",
      "username": "john.doe",
      "email": "john.doe@company.com",
      "first_name": "John",
      "last_name": "Doe",
      "phone": "+1234567890",
      "department": "Engineering",
      "title": "Software Engineer",
      "groups": ["Engineering Team", "All Employees"]
    }
  }
}
```

**Response (User Not Found):**
```json
{
  "success": true,
  "data": {
    "found": false,
    "message": "User not found in LDAP"
  }
}
```

### 4. Sync LDAP User

**Endpoint:** `POST /api/v1/ldap/sync`

**Access:** Authenticated users with `users:update` permission

**Request Body:**
```json
{
  "username": "john.doe"
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": "uuid-here",
    "email": "john.doe@company.com",
    "username": "john.doe",
    "first_name": "John",
    "last_name": "Doe",
    ...
  },
  "message": "User synced successfully from LDAP"
}
```

### 5. Get LDAP Status

**Endpoint:** `GET /api/v1/ldap/status`

**Access:** Authenticated users with `admin:ldap` permission

**Response:**
```json
{
  "success": true,
  "data": {
    "enabled": true,
    "url": "ldaps://ad.company.com:636",
    "base_dn": "dc=company,dc=com",
    "connected": true
  }
}
```

## User Auto-Creation

When a user successfully authenticates via LDAP for the first time:

1. The system searches for the user in LDAP using the configured search filter
2. If found and credentials are valid, the system checks if the user exists in the local database
3. If the user doesn't exist locally, a new account is created with:
   - Email from LDAP `mail` attribute
   - Username from LDAP `sAMAccountName` attribute
   - Name from LDAP `givenName` and `sn` attributes
   - Phone from LDAP `telephoneNumber` attribute
   - A random password (LDAP users don't use local password authentication)
   - `is_active` set to `true`

## User Synchronization

On subsequent logins, the user's local account is updated with the latest LDAP data:
- First name
- Last name
- Phone number
- Account status (enabled/disabled in AD)

## LDAP Attributes Mapped

| LDAP Attribute | Local Field | Description |
|---------------|-------------|-------------|
| `sAMAccountName` | username | Login username |
| `mail` | email | Email address |
| `givenName` | first_name | First name |
| `sn` | last_name | Last name |
| `telephoneNumber` | phone | Phone number |
| `department` | - | Department name |
| `title` | - | Job title |
| `memberOf` | groups | Group memberships |
| `userAccountControl` | is_active | Account enabled status |

## Security Considerations

### TLS/SSL (LDAPS)

For production environments, always use LDAPS (LDAP over SSL):

```env
LDAP_URL=ldaps://ad.company.com:636
LDAP_INSECURE_SKIP_VERIFY=false
```

### Service Account

Create a dedicated service account for LDAP binding with minimal required permissions:
- Read-only access to user objects
- Ability to search the directory

### Firewall Rules

Ensure the backend server can reach the LDAP server:
- Port 389 for LDAP
- Port 636 for LDAPS

## Troubleshooting

### Connection Issues

1. **Cannot connect to LDAP server**
   - Verify the LDAP_URL is correct
   - Check network connectivity (telnet ldap.server.com 389)
   - Ensure firewall allows LDAP traffic

2. **Bind failed**
   - Verify LDAP_BIND_DN and LDAP_BIND_PASSWORD
   - Check if the service account is not locked/expired
   - Ensure the bind DN has correct permissions

3. **User not found**
   - Verify LDAP_USER_SEARCH_BASE is correct
   - Check LDAP_USER_SEARCH_FILTER syntax
   - Ensure the user exists in the specified search base

### Authentication Issues

1. **Invalid credentials**
   - Verify the user's password in AD
   - Check if the user account is not locked/disabled
   - Ensure the user can bind to LDAP directly

2. **Certificate validation failed (LDAPS)**
   - Set `LDAP_INSECURE_SKIP_VERIFY=true` for testing only
   - For production, install the CA certificate on the server

### Debug Mode

Enable LDAP debugging by checking the server logs. The LDAP service logs:
- Connection attempts
- Search queries
- Authentication results

## Integration with Frontend

Update your frontend login page to support LDAP authentication:

```javascript
// Example login with LDAP
async function loginWithLDAP(username, password) {
  const response = await fetch('/api/v1/ldap/login', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ username, password }),
  });
  
  const data = await response.json();
  
  if (data.success) {
    // Store tokens
    localStorage.setItem('token', data.data.token);
    localStorage.setItem('refreshToken', data.data.refresh_token);
    // Redirect to dashboard
  } else {
    // Show error
    alert(data.error);
  }
}
```

## Hybrid Authentication

You can support both LDAP and local authentication:

1. Try LDAP login first (if LDAP_ENABLED=true)
2. If LDAP fails, fall back to local database authentication
3. Or provide a toggle on the login page for users to choose

## Required Permissions

The following permissions control access to LDAP endpoints:

| Endpoint | Required Permission |
|----------|-------------------|
| POST /ldap/login | None (public) |
| POST /ldap/test | `admin:ldap` |
| POST /ldap/search | `users:view` |
| POST /ldap/sync | `users:update` |
| GET /ldap/status | `admin:ldap` |

To grant LDAP admin permissions to a role:

```sql
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.code = 'admin' AND p.code = 'admin:ldap';
```

## Support

For issues or questions about LDAP integration, please refer to the project documentation or contact the development team.
