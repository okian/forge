<!-- Generated from the diagnostic registry. Do not edit.
     Regenerate with: make diagnostics -->

# Diagnostics

Every failure `forge` reports carries a code, and the code is permanent.
Messages get reworded and the rule behind one may be reimplemented, but the
number keeps its meaning — so a suppression, a runbook or a search against one
does not rot.

A report is one line saying what is wrong, the declaration underlined where the
fault is, and a hint saying what to do about it:

```
model/spec.go:12:6: FRG1008: Csv is a transport with Ring written over it
  Ring[Csv[Person]]
       ^^^
  hint: a transport terminates a stack, so write it outermost and write only one
```

The position is the **declaration** rather than the generated file. An author
cannot edit generated code, so a report pointing there would say where the
consequence landed rather than where the cause is.

The first digit places a failure without looking anything up.

## Composition — `FRG1xxx`

The shape of the stack: which layers may sit where, how many of each, and whether each can sit on what is beneath it.

| Code | What it reports |
| ---- | --------------- |
| `FRG1001` | a subject appears in the stack |
| `FRG1002` | element layers are not together around the subject |
| `FRG1003` | two storage layers in stack |
| `FRG1004` | decorator has no storage beneath it |
| `FRG1006` | layer cannot sit on the stack beneath it |
| `FRG1007` | layer takes more than one type argument |
| `FRG1008` | transport is not the outermost layer |
| `FRG1009` | a bridge stands alone over its two types |
| `FRG1020` | a layer appears twice in one stack |
| `FRG1021` | an inline declaration names a layer that cannot be its underlying type |
| `FRG1022` | an inline declaration names more than one layer |
| `FRG1900` | no layer claims this marker |
| `FRG1901` | layer failed while being composed |

## The subject — `FRG2xxx`

The type a stack is specialised to, and what a layer can and cannot make of its fields.

| Code | What it reports |
| ---- | --------------- |
| `FRG2001` | subject is not a named type |
| `FRG2002` | subject is a pointer |
| `FRG2003` | subject is not fully instantiated |
| `FRG2004` | struct tag is malformed |
| `FRG2005` | subject type is missing |
| `FRG2006` | type nests inside itself without end |
| `FRG2007` | field type cannot be encoded without reflection |
| `FRG2008` | json tag option is not supported |
| `FRG2009` | two fields claim one name on the wire |
| `FRG2010` | a member cannot be tested for the value it would be omitted at |
| `FRG2011` | a type declares one half of a codec |
| `FRG2012` | validate tag names a rule that is not one |
| `FRG2013` | validate rule was given a value it does not take |
| `FRG2014` | validate rule cannot be asked of this type |
| `FRG2015` | field type cannot be copied without being told to share it |
| `FRG2016` | a type declares a Clone that is not a copy of itself |
| `FRG2017` | field type cannot be hashed by its content |
| `FRG2018` | a type declares a Hash that is not a content hash |
| `FRG2019` | a type declares a Validate that is not a check |
| `FRG2020` | a builder cannot be given a field it cannot set |
| `FRG2021` | a builder over a subject with nothing a caller can give |
| `FRG2022` | a builder cannot be given a field of this type |
| `FRG2023` | a patch over a subject with nothing a caller can change |
| `FRG2024` | a patch cannot carry a field of this type |
| `FRG2025` | redaction was asked for and nothing is marked secret |
| `FRG2026` | a secret sits behind something a log value cannot mask |
| `FRG2027` | a closed set was asked for over something that is not a named scalar |
| `FRG2028` | a closed set was asked for and no constants are declared of the type |
| `FRG2029` | a closed set was asked for over a type another package declares |
| `FRG2030` | two members of a closed set are called the same thing |
| `FRG2031` | an unexported field carries a json tag |
| `FRG2032` | a struct has no members to write |
| `FRG2033` | a type has no default JSON representation |
| `FRG2034` | a bridge's source has no members to read |
| `FRG2035` | a target member is settled no way |
| `FRG2036` | two source members claim one target member |
| `FRG2037` | a matched member's types do not assign |
| `FRG2038` | a target's unexported fields are out of reach |
| `FRG2039` | a bridge's end is not a named type |

## Directives and options — `FRG3xxx`

What was written on a `//forge:` directive or in a struct tag, judged against what the layer said it accepts.

| Code | What it reports |
| ---- | --------------- |
| `FRG3001` | directive applies to no declaration |
| `FRG3002` | comment resembles a directive but is not one |
| `FRG3003` | directive names no layer |
| `FRG3004` | directive names a layer the declaration does not use |
| `FRG3005` | layer is configured by two directives |
| `FRG3006` | layer has no such option |
| `FRG3007` | option is written twice |
| `FRG3008` | option belongs on a field |
| `FRG3009` | option value is not what the option takes |
| `FRG3010` | option does not name a field of the subject |
| `FRG3011` | layer cannot generate without this option |
| `FRG3012` | option has no name |
| `FRG3013` | field cannot be ordered |
| `FRG3014` | field cannot be a map key |
| `FRG3015` | field cannot be read from the generated package |
| `FRG3016` | option names one field twice |
| `FRG3017` | declared capacity holds nothing |
| `FRG3018` | clone directive on a field is not one |
| `FRG3019` | skip turns nothing off |
| `FRG3020` | skip repeats one already written |
| `FRG3021` | a display tag names a field that cannot be rendered |
| `FRG3022` | a display tag carries an option nothing reads |
| `FRG3023` | a method the subject earns has the name of one of its fields |
| `FRG3024` | hash directive on a field is not one |
| `FRG3025` | a function directive the map layer does not take |
| `FRG3026` | a map hint is not shaped like one |
| `FRG3028` | two hints for one mapping |
| `FRG3029` | a map hint matches no declaration |
| `FRG3030` | a map hint lives outside the spec file |
| `FRG3031` | ignore names a member that is already settled |
| `FRG3032` | a hint says more than a hint may |
| `FRG3033` | one member assigned twice in a hint |

## Emission — `FRG4xxx`

Found while deciding what to write. These are about the output rather than about the declaration: an author has usually done nothing wrong, and the hint says so.

| Code | What it reports |
| ---- | --------------- |
| `FRG4001` | generated source does not parse |
| `FRG4002` | generated declaration is not well formed |
| `FRG4003` | one import path is bound to two names |
| `FRG4004` | build constraint is not a constraint |
| `FRG4005` | generated header cannot be written |
| `FRG4007` | nothing provides a helper a layer requires |
| `FRG4008` | layer could not generate |
| `FRG4009` | a layer's streaming method is not the one a codec is written against |
| `FRG4011` | a method the author declared does not satisfy the contract the stack needs |
| `FRG4012` | two layers want one method name |
| `FRG4013` | a generated declaration collides with one the package already has |
| `FRG4014` | a layer handed over nothing where a declaration should be |
| `FRG4015` | a layer named an import path that is empty |
| `FRG4016` | two import paths bind one package name |
| `FRG4017` | a walk answers with something other than the declaration's elements |
| `FRG4018` | two generated declarations want one package-level name |
| `FRG4019` | a builder's setter wants a name the builder already has |
| `FRG4020` | a patch's field wants a name the patch already has |
| `FRG4021` | two layers disagree about the name an import binds |
| `FRG4101` | two generated names are one |
| `FRG4102` | two fields project to one name |
| `FRG4900` | layer generates nothing yet |
| `FRG4910` | template does not parse |
| `FRG4911` | template is not shaped like a template |
| `FRG4912` | template name collides with what it was rewritten into |

## Input, output and the toolchain — `FRG5xxx`

Loading the packages, reading the tree, and writing the files.

| Code | What it reports |
| ---- | --------------- |
| `FRG5001` | package does not build |
| `FRG5002` | pattern matched no Go files |
| `FRG5003` | generated file belongs to no declaration here |
| `FRG5004` | generated file is out of date |
| `FRG5005` | declaration has no generated file |
| `FRG5006` | a file forge did not write is in the way |
| `FRG5007` | generated file was written by different tooling |
| `FRG5008` | generated file was written by a forge that wrote one file per declaration |

## Layers — `FRG6xxx` and above

Reported by a layer forge does not ship, and not listed here: forge cannot document a code it has never seen. The number belongs to whoever wrote the layer and so does the explanation, and the message names which layer raised it. [`x/csv`](../x/csv) is the one this repository ships beside forge; anything else came from a binary somebody linked a layer into.

