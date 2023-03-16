class muppetshow {
  file { "/data":
    ensure => directory,
  }
  file { "/data/puppet_apply":
    ensure => directory,
  }
  package { 'nginx':
    ensure => installed,
  }
  service { 'nginx':
    ensure => running,
  }
  file { "/var/www/html/index.html":
    ensure => present,
    content => "Muppets\n",
    notify => Service['nginx'],
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

  file { "/data/puppet_apply/the_muppet_show":
    ensure => present,
    content => $the_muppet_show,
  }
  file { "/data/puppet_apply/laughtrack":
    ensure => present,
    content => '',
  }
  file { "/data/puppet_apply/cast":
    ensure => present,
    content => "Cookie Monster\n",
  }
  exec { 'devnull-permadiff':
    command => 'bash -c \'printf "garbage" > /dev/null\'',
    onlyif => 'bash -c \'[[ "garbage" != "$(</dev/null)" ]]\'',
    path => ['/bin'],
  }
}
