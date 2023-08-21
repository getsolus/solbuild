_solbuild()
{
  local cur command commands options recipes files

  COMPREPLY=()
  cur=${COMP_WORDS[COMP_CWORD]}

  commands="build chroot delete-cache help index init update version"

  options="-d --debug -n --no-color -p --profile"
  recipes=""
  files=""

  if [[ $COMP_CWORD -eq 1 ]] ; then
    if [[ "$cur" == -* ]]; then
      COMPREPLY=($(compgen -W "--version --help" -- $cur))
    else
      COMPREPLY=($(compgen -W "$commands --version --help" -- $cur))
    fi
    return 0;
  else
    command=${COMP_WORDS[1]}

    # Completion for global args
    prev=${COMP_WORDS[COMP_CWORD-1]}
    case "$prev" in
    --@(profile))
        if [ `ls /usr/share/solbuild/*.profile /etc/solbuild/*.profile 2> /dev/null | wc -l` -gt 0 ]; then
          files=`ls /usr/share/solbuild/*.profile /etc/solbuild/*.profile | cut -f 1 -d '.' | sed 's#.*/##'`
        fi
        COMPREPLY=($(compgen -W "$files" -- $cur))
        return 0
    ;;
    # FIXME: Duplicated logic
    -@(p))
        if [ `ls /usr/share/solbuild/*.profile /etc/solbuild/*.profile 2> /dev/null | wc -l` -gt 0 ]; then
          files=`ls /usr/share/solbuild/*.profile /etc/solbuild/*.profile | cut -f 1 -d '.' | sed 's#.*/##'`
        fi
        COMPREPLY=($(compgen -W "$files" -- $cur))
        return 0
    ;;
    esac

    # Completion for subcommand specific args
    if [[ "$cur" == -* ]]; then
        case $command in
          @(build))
            options="${options} --tmpfs --memory --transit-manifest --disable-abi-report"
            ;;
          @(delete-cache|dc))
            options="${options} --all --images --sizes"
            ;;
          @(index))
            options="${options} --tmpfs --memory"
            ;;
          @(init))
            options="${options} --update"
            ;;
        esac
        COMPREPLY=($(compgen -W "$options" -- $cur))
        return 0;
    else
        case $command in
          @(build|chroot))
            if [ `ls package.yml 2> /dev/null | wc -l` -gt 0 ]; then
              recipes="package.yml"
            elif [ `ls pspec.xml 2> /dev/null | wc -l` -gt 0 ]; then
              recipes="pspec.xml"
            elif [ `ls *.yml *.yaml 2> /dev/null | wc -l` -gt 0 ]; then
              recipes="$(ls *.yml *.yaml)"
            fi
            COMPREPLY=($(compgen -W "$recipes" -- $cur))
            return 0;
            ;;
        esac
        COMPREPLY=($(compgen -f -- $cur))
        return 0;
    fi
  fi
  _filedir '@(solbuild)'
}
complete -F _solbuild -o filenames solbuild



