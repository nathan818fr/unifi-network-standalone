# Managing users

You manage users in Network's web interface. These are for when you can't log in as an admin.

## On Home Assistant

In the app's **Configuration**, set **Reset an admin** to a user's name or email, and restart the app.
That user gets admin rights and a temporary password, shown in the app's **Log** tab. Once logged in,
change the password, and empty the setting. Setting the same user again only applies after that.

The same commands as in a container are also available, with an SSH app with its protection mode turned
off:

```sh
docker exec -it "$(docker ps -qf name=unifi_network_standalone)" unifi-users list
```

## In a container

```sh
podman exec -it unifi unifi-users list                  # Every user: password, rights, last login
podman exec -it unifi unifi-users set-password <user>   # Asks for a password, changes nothing else
podman exec -it unifi unifi-users grant-admin <user>    # Admin rights on every site, after confirming
```

`<user>` is a name, an email, or an id from the list when several users share a name.
