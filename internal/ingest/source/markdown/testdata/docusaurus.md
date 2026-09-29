---
sidebar_position: 1
title: Supported Types Page
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# 📊 Supported Types {#supported-types}

Data is stored using string keys where slashes are used to denote subtables (similar to NetworkTables).

### Structured {#structured}

Many WPILib classes can be serialized to binary data using structs or protobufs.

:::danger
Protobuf logging can take an extended period (>100ms) the first time that a value with any given type is logged.
:::

<Tabs>
<TabItem value="java" label="Java">

```java title="Robot.java"
Logger.recordOutput("MyPose", pose);
```

</TabItem>
<TabItem value="windows" label="Windows">

Run the installer from the start menu and follow the prompts shown.

</TabItem>
</Tabs>
