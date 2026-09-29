# Follow a Single Path

## Using AutoBuilder

The easiest way to create a command to follow a single path is by using AutoBuilder.

> **Note**
>
> You must have previously configured AutoBuilder in order to use this option.
>
{style="note"}

<tabs group="pplib-language">
<tab title="Java" group-key="java">

```Java
PathPlannerPath path = PathPlannerPath.fromPathFile("Example Path");
return AutoBuilder.followPath(path);
```

</tab>
<tab title="C++" group-key="cpp">

```C++
auto path = PathPlannerPath::fromPathFile("Example Path");
return AutoBuilder::followPath(path);
```

</tab>
</tabs>
