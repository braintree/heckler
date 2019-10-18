File {
  backup => false,
}

node default {
  include $facts['nodename']
}


class fozzie {
  include muppets

  file { "${facts['cwd']}/nodes/fozzie":
    ensure => directory,
  }
  file { "${facts['cwd']}/nodes/fozzie/slapstick":
    ensure => present,
    content => "wacka wacka!\n",
  }
}

class statler {
  include muppets

  file { "${facts['cwd']}/nodes/statler":
    ensure => directory,
  }

  file { "${facts['cwd']}/nodes/statler/wit":
    ensure => present,
    content => "terrible\n",
  }
}

class waldorf {
  include muppets

  file { "${facts['cwd']}/nodes/waldorf":
    ensure => directory,
  }
  file { "${facts['cwd']}/nodes/waldorf/poignant":
    ensure => present,
    content => "acerbic\n",
  }
}

class muppets {
  file { "${facts['cwd']}/nodes":
    ensure => directory,
  }
  package { 'nginx':
    ensure => installed,
  }
  $the_muppet_show = @(EOF)
    It's the Muppet Show

    It's time to play the music
    It's time to light the lights
    It's time to meet the Muppets on the Muppet Show tonight
    It's time to put on make up
    It's time to dress up right
    It's time to raise the curtain on the Muppet Show tonight
    | EOF

  file { "${facts['cwd']}/nodes/the_muppet_show":
    ensure => present,
    content => $the_muppet_show,
  }
}
