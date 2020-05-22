## Puppet noop output for commit: 1eba2f8789a532c0d5bee1218d51ec5766229030 - stop nginx on fozzie & add episode one

### Commit [1eba2f8789a532c0d5bee1218d51ec5766229030](https://github.braintreeps.com/braintree/muppetshow/commit/1eba2f8789a532c0d5bee1218d51ec5766229030)

- *Author:* kermit (<heckler+kermit@getbraintree.com>)
- *Message:*
  ```
  stop nginx on fozzie & add episode one
  
  stop nginx on fozzie
  add episode one
  modify wit on statler
  modify poignant on waldor
  modify slapstick on fozzie
  
  ```

### Puppet Resource Changes (noop)

-  **File[/data/puppet_apply/cast]**
    - *Diff:*
      ```diff
      @@ -1 +1,2 @@
      -Cookie Monster
      +Miss Piggy
      +Rowlf the Dog
      ```
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Define Type:* `Concat[/data/puppet_apply/cast]`
    - *Current State:* `{md5}9a21a9ced788104a4f937ebd113c62d3`
    - *Desired State:* `{md5}65fddd5ddf8227027bc41117b09ada27`

-  **File[/data/puppet_apply/fozzie/slapstick]**
    - *Hosts:* `fozzie.example.com`
    - *Source File:* [modules/fozzie/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/1eba2f8789a532c0d5bee1218d51ec5766229030/modules/fozzie/manifests/init.pp#L5)
    - *Current State:* `file`
    - *Desired State:* `absent`

-  **File[/data/puppet_apply/laughtrack]**
    - *Diff:*
      ```diff
      @@ -0,0 +1,2 @@
      +Wacka
      +Wacka
      ```
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Define Type:* `Muppetshow::Episode[One]`
    - *Source File:* [modules/muppetshow/manifests/episode.pp](https://github.braintreeps.com/braintree/muppetshow/blob/1eba2f8789a532c0d5bee1218d51ec5766229030/modules/muppetshow/manifests/episode.pp#L4)
    - *Current State:* `{md5}d41d8cd98f00b204e9800998ecf8427e`
    - *Desired State:* `{md5}15d4a7e90a35a1b9d8d69deecbf9f7d0`

-  **File[/data/puppet_apply/statler/wit]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -terrible
      +foul
      ```
    - *Hosts:* `statler.example.com`
    - *Source File:* [modules/statler/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/1eba2f8789a532c0d5bee1218d51ec5766229030/modules/statler/manifests/init.pp#L8)
    - *Current State:* `{md5}114457bbbd50c0aca9f294e02c4ca712`
    - *Desired State:* `{md5}0476788129760a948a9b6a0e5275e965`

-  **File[/data/puppet_apply/waldorf/poignant]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -acerbic
      +sour
      ```
    - *Hosts:* `waldorf.example.com`
    - *Source File:* [modules/waldorf/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/1eba2f8789a532c0d5bee1218d51ec5766229030/modules/waldorf/manifests/init.pp#L7)
    - *Current State:* `{md5}2af84c70543edf1599b4eccdc65247c0`
    - *Desired State:* `{md5}4ac18c74645273475594a9c255e321f0`

-  **Service[nginx]**
    - *Hosts:* `fozzie.example.com`
    - *Source File:* [modules/fozzie/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/1eba2f8789a532c0d5bee1218d51ec5766229030/modules/fozzie/manifests/init.pp#L8)
    - *Current State:* `running`
    - *Desired State:* `stopped`

## Puppet noop output for commit: 093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6 - finish the muppet show lyrics

### Commit [093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6](https://github.braintreeps.com/braintree/muppetshow/commit/093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6)

- *Author:* misspiggy (<heckler+misspiggy@getbraintree.com>)
- *Message:*
  ```
  finish the muppet show lyrics
  
  finish composing the muppet show lyrics
  move index out of muppetshow class into node
  class
  
  ```

### Puppet Resource Changes (noop)

-  **File[/data/puppet_apply/the_muppet_show]**
    - *Diff:*
      ```diff
      @@ -6,3 +6,14 @@
       It's time to put on make up
       It's time to dress up right
       It's time to raise the curtain on the Muppet Show tonight
      +
      +Why do we always come here
      +I guess we'll never know
      +It's like a kind of torture
      +To have to watch the show
      +
      +But now let's get things started
      +Why don't you get things started
      +It's time to get things started
      +On the most sensational, inspirational, celebrational, muppetational
      +This is what we call the Muppet Show
      ```
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Source File:* [modules/muppetshow/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6/modules/muppetshow/manifests/init.pp#L33)
    - *Current State:* `{md5}eb554c301d10b04fb72261d721990270`
    - *Desired State:* `{md5}52a949d5beed1782e28094c8a74b8ac1`

-  **File[/var/www/html/index.html]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -Muppets
      +Fozzie
      ```
    - *Hosts:* `fozzie.example.com`
    - *Source File:* [modules/fozzie/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6/modules/fozzie/manifests/init.pp#L11)
    - *Current State:* `{md5}e5deafb6425abb47e5a1efef8b969fb8`
    - *Desired State:* `{md5}a148678410c6e6abb9fe0103391f3692`

-  **File[/var/www/html/index.html]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -Muppets
      +Statler
      ```
    - *Hosts:* `statler.example.com`
    - *Source File:* [modules/statler/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6/modules/statler/manifests/init.pp#L15)
    - *Current State:* `{md5}e5deafb6425abb47e5a1efef8b969fb8`
    - *Desired State:* `{md5}2599d35b2c0893c7dea8a07c372b02a0`

-  **File[/var/www/html/index.html]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -Muppets
      +Waldorf
      ```
    - *Hosts:* `waldorf.example.com`
    - *Source File:* [modules/waldorf/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6/modules/waldorf/manifests/init.pp#L14)
    - *Current State:* `{md5}e5deafb6425abb47e5a1efef8b969fb8`
    - *Desired State:* `{md5}afc384a68e4fae8b8d257e15cd167a48`

-  **Service[nginx]**
    - *Hosts:* `statler.example.com`
    - *Source File:* [modules/statler/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6/modules/statler/manifests/init.pp#L12)
    - *Logs:*
        - `Would have triggered 'refresh' from 1 event`

-  **Service[nginx]**
    - *Hosts:* `waldorf.example.com`
    - *Source File:* [modules/waldorf/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/093f5e07882fdb95bdf6b94366ec2a32fdb3f1e6/modules/waldorf/manifests/init.pp#L11)
    - *Logs:*
        - `Would have triggered 'refresh' from 1 event`

## Puppet noop output for commit: 98f94001102274c1f25bc94e1b919f7b654ded4b - add some fun diversions

### Commit [98f94001102274c1f25bc94e1b919f7b654ded4b](https://github.braintreeps.com/braintree/muppetshow/commit/98f94001102274c1f25bc94e1b919f7b654ded4b)

- *Author:* kermit (<heckler+kermit@getbraintree.com>)
- *Message:*
  ```
  add some fun diversions
  
  add bsdgames on fozzie
  add sl to statler & waldorf
  
  ```

### Puppet Resource Changes (noop)

-  **Package[bsdgames]**
    - *Hosts:* `fozzie.example.com`
    - *Source File:* [modules/fozzie/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/98f94001102274c1f25bc94e1b919f7b654ded4b/modules/fozzie/manifests/init.pp#L15)
    - *Current State:* `purged`
    - *Desired State:* `present`

-  **Package[sl]**
    - *Hosts:* `statler.example.com`
    - *Source File:* [modules/statler/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/98f94001102274c1f25bc94e1b919f7b654ded4b/modules/statler/manifests/init.pp#L20)
    - *Current State:* `purged`
    - *Desired State:* `present`

-  **Package[sl]**
    - *Hosts:* `waldorf.example.com`
    - *Source File:* [modules/waldorf/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/98f94001102274c1f25bc94e1b919f7b654ded4b/modules/waldorf/manifests/init.pp#L19)
    - *Current State:* `purged`
    - *Desired State:* `present`

## Puppet noop output for commit: 71513759d5ce2d2e17a7390707c77cbdc72b6929 - add kermit user, modify sail input

### Commit [71513759d5ce2d2e17a7390707c77cbdc72b6929](https://github.braintreeps.com/braintree/muppetshow/commit/71513759d5ce2d2e17a7390707c77cbdc72b6929)

- *Author:* kermit (<heckler+kermit@getbraintree.com>)
- *Message:*
  ```
  add kermit user, modify sail input
  
  add kermit user and muppetshow group
  modify the input to the sail game
  
  ```

### Puppet Resource Changes (noop)

-  **Exec[sail]**
    - *Hosts:* `fozzie.example.com`
    - *Source File:* [modules/fozzie/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/71513759d5ce2d2e17a7390707c77cbdc72b6929/modules/fozzie/manifests/init.pp#L22)
    - *Logs:*
        - `Would have triggered 'refresh' from 1 event`

-  **File[/data/puppet_apply/fozzie/styx]**
    - *Diff:*
      ```diff
      @@ -0,0 +1 @@
      +Come Sail Away
      ```
    - *Hosts:* `fozzie.example.com`
    - *Source File:* [modules/fozzie/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/71513759d5ce2d2e17a7390707c77cbdc72b6929/modules/fozzie/manifests/init.pp#L18)
    - *Current State:* `{md5}d41d8cd98f00b204e9800998ecf8427e`
    - *Desired State:* `{md5}d256d47934e09aa4e80b8e6e5b5519c4`

-  **Group[kermit]**
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Source File:* [modules/muppetshow/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/71513759d5ce2d2e17a7390707c77cbdc72b6929/modules/muppetshow/manifests/init.pp#L38)
    - *Current State:* `absent`
    - *Desired State:* `present`

-  **Group[muppets]**
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Source File:* [modules/muppetshow/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/71513759d5ce2d2e17a7390707c77cbdc72b6929/modules/muppetshow/manifests/init.pp#L49)
    - *Current State:* `absent`
    - *Desired State:* `present`

-  **User[kermit]**
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Source File:* [modules/muppetshow/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/71513759d5ce2d2e17a7390707c77cbdc72b6929/modules/muppetshow/manifests/init.pp#L42)
    - *Current State:* `absent`
    - *Desired State:* `present`

## Puppet noop output for commit: 0ae719430b34ff2cb487a9a03bc59c7383de8993 - New Movie

### Commit [0ae719430b34ff2cb487a9a03bc59c7383de8993](https://github.braintreeps.com/braintree/muppetshow/commit/0ae719430b34ff2cb487a9a03bc59c7383de8993)

- *Author:* misspiggy (<heckler+misspiggy@getbraintree.com>)
- *Message:*
  ```
  New Movie
  
  ```

### Puppet Resource Changes (noop)

-  **File[/data/puppet_apply/fozzie/manhattan]**
    - *Hosts:* `fozzie.example.com`
    - *Source File:* [modules/fozzie/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/0ae719430b34ff2cb487a9a03bc59c7383de8993/modules/fozzie/manifests/init.pp#L11)
    - *Current State:* `absent`
    - *Desired State:* `present`

## Puppet noop output for commit: 5836064c1146d628eda8feb6cae99d7870e5ea90 - Gonzo

### Commit [5836064c1146d628eda8feb6cae99d7870e5ea90](https://github.braintreeps.com/braintree/muppetshow/commit/5836064c1146d628eda8feb6cae99d7870e5ea90)

- *Author:* misspiggy (<heckler+misspiggy@getbraintree.com>)
- *Message:*
  ```
  Gonzo
  
  ```

### Puppet Resource Changes (noop)

-  **File[/data/puppet_apply/waldorf/manhattan]**
    - *Hosts:* `waldorf.example.com`
    - *Source File:* [modules/waldorf/manifests/init.pp](https://github.braintreeps.com/braintree/muppetshow/blob/5836064c1146d628eda8feb6cae99d7870e5ea90/modules/waldorf/manifests/init.pp#L11)
    - *Current State:* `absent`
    - *Desired State:* `present`

## Puppet noop output for commit: 57847d332f4ea61ad3f62111a57356877619d4d0 - Take Manhattan

### Commit [57847d332f4ea61ad3f62111a57356877619d4d0](https://github.braintreeps.com/braintree/muppetshow/commit/57847d332f4ea61ad3f62111a57356877619d4d0)

- *Author:* Jesse Hathaway (<hathaway@paypal.com>)
- *Message:*
  ```
  Take Manhattan
  
  ```

### Puppet Resource Changes (noop)

No resource changes generated by this commit
