## Puppet noop output for commit: 3db0665e2ff0fef1268ec1df274cbe0f920f538d - stop nginx on fozzie & add episode one

### Commit [3db0665e2ff0fef1268ec1df274cbe0f920f538d](https://github.braintreeps.com/lollipopman/muppetshow/commit/3db0665e2ff0fef1268ec1df274cbe0f920f538d)

- *Author:* lollipopman (<hathaway@paypal.com>)
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

1.  **File[/data/puppet_apply/cast]**
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

2.  **File[/data/puppet_apply/fozzie/slapstick]**
    - *Hosts:* `fozzie.example.com`
    - *Current State:* `file`
    - *Desired State:* `absent`

3.  **File[/data/puppet_apply/laughtrack]**
    - *Diff:*
      ```diff
      @@ -0,0 +1,2 @@
      +Wacka
      +Wacka
      ```
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Define Type:* `Muppetshow::Episode[One]`
    - *Current State:* `{md5}d41d8cd98f00b204e9800998ecf8427e`
    - *Desired State:* `{md5}15d4a7e90a35a1b9d8d69deecbf9f7d0`

4.  **File[/data/puppet_apply/statler/wit]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -terrible
      +foul
      ```
    - *Hosts:* `statler.example.com`
    - *Current State:* `{md5}114457bbbd50c0aca9f294e02c4ca712`
    - *Desired State:* `{md5}0476788129760a948a9b6a0e5275e965`

5.  **File[/data/puppet_apply/waldorf/poignant]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -acerbic
      +sour
      ```
    - *Hosts:* `waldorf.example.com`
    - *Current State:* `{md5}2af84c70543edf1599b4eccdc65247c0`
    - *Desired State:* `{md5}4ac18c74645273475594a9c255e321f0`

6.  **Service[nginx]**
    - *Hosts:* `fozzie.example.com`
    - *Current State:* `running`
    - *Desired State:* `stopped`

## Puppet noop output for commit: b36edf5ec8105a365b068aab9f6d4dce15dab6d3 - finish the muppet show lyrics

### Commit [b36edf5ec8105a365b068aab9f6d4dce15dab6d3](https://github.braintreeps.com/lollipopman/muppetshow/commit/b36edf5ec8105a365b068aab9f6d4dce15dab6d3)

- *Author:* lollipopman (<hathaway@paypal.com>)
- *Message:*
  ```
  finish the muppet show lyrics
  
  finish composing the muppet show lyrics
  move index out of muppetshow class into node
  class
  
  ```

### Puppet Resource Changes (noop)

1.  **File[/data/puppet_apply/the_muppet_show]**
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
    - *Current State:* `{md5}eb554c301d10b04fb72261d721990270`
    - *Desired State:* `{md5}52a949d5beed1782e28094c8a74b8ac1`

2.  **File[/var/www/html/index.html]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -Muppets
      +Fozzie
      ```
    - *Hosts:* `fozzie.example.com`
    - *Current State:* `{md5}e5deafb6425abb47e5a1efef8b969fb8`
    - *Desired State:* `{md5}a148678410c6e6abb9fe0103391f3692`

3.  **File[/var/www/html/index.html]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -Muppets
      +Statler
      ```
    - *Hosts:* `statler.example.com`
    - *Current State:* `{md5}e5deafb6425abb47e5a1efef8b969fb8`
    - *Desired State:* `{md5}2599d35b2c0893c7dea8a07c372b02a0`

4.  **File[/var/www/html/index.html]**
    - *Diff:*
      ```diff
      @@ -1 +1 @@
      -Muppets
      +Waldorf
      ```
    - *Hosts:* `waldorf.example.com`
    - *Current State:* `{md5}e5deafb6425abb47e5a1efef8b969fb8`
    - *Desired State:* `{md5}afc384a68e4fae8b8d257e15cd167a48`

5.  **Service[nginx]**
    - *Hosts:* `{statler,waldorf}.example.com`
    - *Logs:*
        - `Would have triggered 'refresh' from 1 event`

## Puppet noop output for commit: 544bd51715e99dd78485f59ffc115d2fd04ae3b7 - add some fun diversions

### Commit [544bd51715e99dd78485f59ffc115d2fd04ae3b7](https://github.braintreeps.com/lollipopman/muppetshow/commit/544bd51715e99dd78485f59ffc115d2fd04ae3b7)

- *Author:* lollipopman (<hathaway@paypal.com>)
- *Message:*
  ```
  add some fun diversions
  
  add bsdgames on fozzie
  add sl to statler & waldorf
  
  ```

### Puppet Resource Changes (noop)

1.  **Package[bsdgames]**
    - *Hosts:* `fozzie.example.com`
    - *Current State:* `purged`
    - *Desired State:* `present`

2.  **Package[sl]**
    - *Hosts:* `{statler,waldorf}.example.com`
    - *Current State:* `purged`
    - *Desired State:* `present`

## Puppet noop output for commit: ebd776c700e015b6feda6e1ce2e4cf31aad4dba1 - add kermit user, modify sail input

### Commit [ebd776c700e015b6feda6e1ce2e4cf31aad4dba1](https://github.braintreeps.com/lollipopman/muppetshow/commit/ebd776c700e015b6feda6e1ce2e4cf31aad4dba1)

- *Author:* lollipopman (<hathaway@paypal.com>)
- *Message:*
  ```
  add kermit user, modify sail input
  
  add kermit user and muppetshow group
  modify the input to the sail game
  
  ```

### Puppet Resource Changes (noop)

1.  **Exec[sail]**
    - *Hosts:* `fozzie.example.com`
    - *Logs:*
        - `Would have triggered 'refresh' from 1 event`

2.  **File[/data/puppet_apply/fozzie/styx]**
    - *Diff:*
      ```diff
      @@ -0,0 +1 @@
      +Come Sail Away
      ```
    - *Hosts:* `fozzie.example.com`
    - *Current State:* `{md5}d41d8cd98f00b204e9800998ecf8427e`
    - *Desired State:* `{md5}d256d47934e09aa4e80b8e6e5b5519c4`

3.  **Group[kermit]**
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Current State:* `absent`
    - *Desired State:* `present`

4.  **Group[muppets]**
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Current State:* `absent`
    - *Desired State:* `present`

5.  **User[kermit]**
    - *Hosts:* `{fozzie,statler,waldorf}.example.com`
    - *Current State:* `absent`
    - *Desired State:* `present`

## Puppet noop output for commit: aaa8595480615c1d85b19b568626576fa11b774d - New Movie

### Commit [aaa8595480615c1d85b19b568626576fa11b774d](https://github.braintreeps.com/lollipopman/muppetshow/commit/aaa8595480615c1d85b19b568626576fa11b774d)

- *Author:* lollipopman (<hathaway@paypal.com>)
- *Message:*
  ```
  New Movie
  
  ```

### Puppet Resource Changes (noop)

1.  **File[/data/puppet_apply/fozzie/manhattan]**
    - *Hosts:* `fozzie.example.com`
    - *Current State:* `absent`
    - *Desired State:* `present`

## Puppet noop output for commit: d311e0adc363b5327a3e14c8d20f535d289a8a26 - Gonzo

### Commit [d311e0adc363b5327a3e14c8d20f535d289a8a26](https://github.braintreeps.com/lollipopman/muppetshow/commit/d311e0adc363b5327a3e14c8d20f535d289a8a26)

- *Author:* lollipopman (<hathaway@paypal.com>)
- *Message:*
  ```
  Gonzo
  
  ```

### Puppet Resource Changes (noop)

1.  **File[/data/puppet_apply/waldorf/manhattan]**
    - *Hosts:* `waldorf.example.com`
    - *Current State:* `absent`
    - *Desired State:* `present`

## Puppet noop output for commit: 0b3da0bdd5aa061444f8918011dbcddb6703b759 - Take Manhattan

### Commit [0b3da0bdd5aa061444f8918011dbcddb6703b759](https://github.braintreeps.com/lollipopman/muppetshow/commit/0b3da0bdd5aa061444f8918011dbcddb6703b759)

- *Author:* lollipopman (<code@getbraintree.com>)
- *Message:*
  ```
  Take Manhattan
  
  ```

### Puppet Resource Changes (noop)

No resource changes generated by this commit
